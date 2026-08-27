package server

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

func (s *Server) CreatePayout(ctx context.Context, req *connect.Request[v1.CreatePayoutRequest]) (*connect.Response[v1.CreatePayoutResponse], error) {
	childID := req.Msg.GetChildId()
	if childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child_id is required"))
	}
	child, err := s.getUser(ctx, childID)
	if err != nil {
		return nil, err
	}
	if err := s.requireParent(ctx, child.FamilyId); err != nil {
		return nil, err
	}
	if child.Role != v1.UserRole_USER_ROLE_CHILD {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("payouts can only be made to children"))
	}

	id := newID()
	now := nowUTC()
	amount := req.Msg.GetAmount().GetCents()

	// Reading the balance and recording against it has to be one atomic
	// step. Split across two statements, two concurrent "pay the full
	// balance" taps both read the same figure and both insert, paying the
	// child twice and leaving them owing the difference. The single
	// connection serializes each statement but not the pair.
	notPositive := errors.New("amount must be greater than zero")
	tooMuch := errors.New("amount exceeds the child's outstanding balance")
	err = s.inTx(ctx, func(q querier) error {
		balance, err := s.childBalanceCents(ctx, q, childID)
		if err != nil {
			return err
		}
		if req.Msg.GetFullPayout() {
			amount = balance
		}
		if amount <= 0 {
			return notPositive
		}
		if amount > balance {
			return tooMuch
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO payouts (id, child_id, family_id, amount_cents, full_payout, note, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, childID, child.FamilyId, amount, req.Msg.GetFullPayout(), req.Msg.GetNote(), formatTime(now),
		); err != nil {
			return fmt.Errorf("create payout: %w", err)
		}
		return nil
	})
	if errors.Is(err, notPositive) || errors.Is(err, tooMuch) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&v1.CreatePayoutResponse{
		Payout: &v1.Payout{
			Id: id, ChildId: childID, FamilyId: child.FamilyId, Amount: money(amount),
			FullPayout: req.Msg.GetFullPayout(), Note: req.Msg.GetNote(), CreatedAt: timestampPB(now),
		},
	}), nil
}

func (s *Server) ListPayouts(ctx context.Context, req *connect.Request[v1.ListPayoutsRequest]) (*connect.Response[v1.ListPayoutsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, req.Msg.GetChildId())
	if err != nil {
		return nil, err
	}
	query := `SELECT id, child_id, family_id, amount_cents, full_payout, note, created_at FROM payouts WHERE family_id = ?`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND child_id = ?`
		args = append(args, childFilter)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var payouts []*v1.Payout
	for rows.Next() {
		var p v1.Payout
		var createdAt string
		var amountCents int64
		if err := rows.Scan(&p.Id, &p.ChildId, &p.FamilyId, &amountCents, &p.FullPayout, &p.Note, &createdAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		p.Amount = money(amountCents)
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		p.CreatedAt = timestampPB(t)
		payouts = append(payouts, &p)
	}
	return connect.NewResponse(&v1.ListPayoutsResponse{Payouts: payouts}), nil
}
