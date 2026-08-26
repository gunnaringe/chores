package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

const invitationTTL = 7 * 24 * time.Hour

// GetMyMembership returns every user row the caller's login identity is
// bound to. Usually that's at most one (a parent belongs to one household),
// but a child can be bound to several — e.g. one per household they split
// time between. A freshly logged-in identity with no matching rows yet
// (hasn't created or joined a family) legitimately reports Bound: false.
func (s *Server) GetMyMembership(ctx context.Context, _ *connect.Request[v1.GetMyMembershipRequest]) (*connect.Response[v1.GetMyMembershipResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE auth_subject = ? ORDER BY created_at`, identity.Sub)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rows.Close()

	if len(userIDs) == 0 {
		return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: false}), nil
	}

	memberships := make([]*v1.Membership, 0, len(userIDs))
	for _, userID := range userIDs {
		user, err := s.getUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		family, err := s.getFamily(ctx, user.FamilyId)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, &v1.Membership{User: user, Family: family})
	}
	return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: true, Memberships: memberships}), nil
}

// CreateInvitation creates an unclaimed parent slot in the family and an
// unguessable one-time token that binds a login identity to it once
// accepted. The token is only ever returned here — ListInvitations omits it.
func (s *Server) CreateInvitation(ctx context.Context, req *connect.Request[v1.CreateInvitationRequest]) (*connect.Response[v1.CreateInvitationResponse], error) {
	familyID := req.Msg.GetFamilyId()
	name := req.Msg.GetName()
	if familyID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and name are required"))
	}
	roleStr, err := roleToDB(req.Msg.GetRole())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("role must be parent or child: %w", err))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}

	now := nowUTC()
	uid := newID()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, family_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		uid, familyID, name, roleStr, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create invited slot: %w", err))
	}

	token := newID()
	invID := newID()
	expiresAt := now.Add(invitationTTL)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO invitations (id, family_id, user_id, token, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		invID, familyID, uid, token, formatTime(now), formatTime(expiresAt),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create invitation: %w", err))
	}

	return connect.NewResponse(&v1.CreateInvitationResponse{
		Invitation: &v1.Invitation{
			Id: invID, FamilyId: familyID, UserId: uid, UserName: name,
			CreatedAt: timestampPB(now), ExpiresAt: timestampPB(expiresAt), Role: req.Msg.GetRole(),
			Token: token,
		},
		Token:      token,
		AcceptPath: "/invite/accept?token=" + token,
	}), nil
}

func (s *Server) ListInvitations(ctx context.Context, req *connect.Request[v1.ListInvitationsRequest]) (*connect.Response[v1.ListInvitationsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.family_id, i.user_id, u.name, u.role, i.created_at, i.expires_at, i.accepted_at, i.token
		 FROM invitations i JOIN users u ON u.id = i.user_id
		 WHERE i.family_id = ? ORDER BY i.created_at DESC`,
		familyID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var invitations []*v1.Invitation
	for rows.Next() {
		var inv v1.Invitation
		var role, createdAt, expiresAt, token string
		var acceptedAt sql.NullString
		if err := rows.Scan(&inv.Id, &inv.FamilyId, &inv.UserId, &inv.UserName, &role, &createdAt, &expiresAt, &acceptedAt, &token); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		inv.Role = roleFromDB(role)
		ct, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		et, err := parseTime(expiresAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		inv.CreatedAt = timestampPB(ct)
		inv.ExpiresAt = timestampPB(et)
		if acceptedAt.Valid {
			at, err := parseTime(acceptedAt.String)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			inv.AcceptedAt = timestampPB(at)
		} else {
			// Only a still-pending invitation's token is any use to anyone —
			// once accepted it can never bind another login, so it's left
			// out rather than needlessly exposing a spent credential.
			inv.Token = token
		}
		invitations = append(invitations, &inv)
	}
	return connect.NewResponse(&v1.ListInvitationsResponse{Invitations: invitations}), nil
}

// RevokeInvitation deletes a not-yet-accepted invitation along with the
// unclaimed parent slot it created.
func (s *Server) RevokeInvitation(ctx context.Context, req *connect.Request[v1.RevokeInvitationRequest]) (*connect.Response[v1.RevokeInvitationResponse], error) {
	invID := req.Msg.GetInvitationId()
	if invID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invitation_id is required"))
	}

	var familyID, userID string
	var acceptedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT family_id, user_id, accepted_at FROM invitations WHERE id = ?`, invID).
		Scan(&familyID, &userID, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invitation not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	if acceptedAt.Valid {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation was already accepted"))
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE id = ?`, invID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke invitation: %w", err))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND auth_subject IS NULL`, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove unclaimed slot: %w", err))
	}
	return connect.NewResponse(&v1.RevokeInvitationResponse{}), nil
}

// AcceptInvitation binds the caller's login identity to the invitation's
// parent slot. Possession of the token is what grants the claim — it isn't
// matched against any particular email address, since a login's verified
// email may differ from whatever address the invite was shared with.
func (s *Server) AcceptInvitation(ctx context.Context, req *connect.Request[v1.AcceptInvitationRequest]) (*connect.Response[v1.AcceptInvitationResponse], error) {
	token := req.Msg.GetToken()
	if token == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("token is required"))
	}
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	var invID, familyID, userID, expiresAtStr string
	var acceptedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT i.id, i.family_id, i.user_id, i.expires_at, i.accepted_at
		 FROM invitations i
		 WHERE i.token = ?`, token,
	).Scan(&invID, &familyID, &userID, &expiresAtStr, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invitation not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if acceptedAt.Valid {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation has already been used"))
	}
	expiresAt, err := parseTime(expiresAtStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if time.Now().After(expiresAt) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation has expired"))
	}

	// A login can be bound to the same family only once — otherwise both
	// parents and children can belong to as many families as they've been
	// invited into (e.g. a parent co-running two households, or a child
	// split between them).
	var existing string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ? AND family_id = ?`, identity.Sub, familyID).Scan(&existing)
	if err == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you are already a member of this family"))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET auth_subject = ?, email = ? WHERE id = ? AND auth_subject IS NULL`,
		identity.Sub, identity.Email, userID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bind invited parent: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation slot has already been claimed"))
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE invitations SET accepted_at = ? WHERE id = ?`, formatTime(nowUTC()), invID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mark invitation accepted: %w", err))
	}

	user, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	family, err := s.getFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.AcceptInvitationResponse{User: user, Family: family}), nil
}
