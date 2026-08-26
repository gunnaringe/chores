package server

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

// mondayOfWeek returns the calendar date (midnight, t's own location) of
// the Monday on or before t — the Monday-first week boundary the UI shows
// elsewhere, as opposed to a rolling 7-day window.
func mondayOfWeek(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	daysSinceMonday := (int(t.Weekday()) + 6) % 7 // Mon=0 .. Sun=6
	return t.AddDate(0, 0, -daysSinceMonday)
}

// earnedClause sums a child's earnings over whatever extra condition the
// caller adds. The completed_at IS NOT NULL filter is not optional and not
// a detail: task_occurrences holds rows for chores that were due and never
// done, and those carry an amount too. Summing without it credits every
// child for work nobody did. It also matches the partial indexes in
// schema.sql, so each of these is answered from an index alone.
const earnedClause = `SELECT COALESCE(SUM(amount_cents), 0) FROM task_occurrences
	WHERE child_id = ? AND completed_at IS NOT NULL`

// childBalanceCents is what the child is currently owed: everything they
// have earned, less everything already paid out. Takes a querier so a payout
// can read it inside the same transaction that records against it.
func (s *Server) childBalanceCents(ctx context.Context, q querier, childID string) (int64, error) {
	var earned, paidOut sql.NullInt64
	if err := q.QueryRowContext(ctx, earnedClause, childID).Scan(&earned); err != nil {
		return 0, err
	}
	if err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payouts WHERE child_id = ?`, childID,
	).Scan(&paidOut); err != nil {
		return 0, err
	}
	return earned.Int64 - paidOut.Int64, nil
}

func (s *Server) computeSummary(ctx context.Context, child *v1.User) (*v1.ChildSummary, error) {
	var totalEarned, earnedLast7Days, earnedToday, earnedThisWeek, totalPaidOut sql.NullInt64
	sevenDaysAgo := formatTime(nowUTC().AddDate(0, 0, -7))
	today := scheduling.FormatDate(nowUTC())
	startOfWeek := scheduling.FormatDate(mondayOfWeek(nowUTC()))

	if err := s.db.QueryRowContext(ctx, earnedClause, child.Id).Scan(&totalEarned); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		earnedClause+` AND completed_at >= ?`,
		child.Id, sevenDaysAgo,
	).Scan(&earnedLast7Days); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		earnedClause+` AND due_date = ?`,
		child.Id, today,
	).Scan(&earnedToday); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		earnedClause+` AND due_date >= ?`,
		child.Id, startOfWeek,
	).Scan(&earnedThisWeek); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payouts WHERE child_id = ?`,
		child.Id,
	).Scan(&totalPaidOut); err != nil {
		return nil, err
	}

	var lastPayoutAt sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM payouts WHERE child_id = ?`,
		child.Id,
	).Scan(&lastPayoutAt); err != nil {
		return nil, err
	}

	summary := &v1.ChildSummary{
		Child:            child,
		EarnedLast_7Days: money(earnedLast7Days.Int64),
		EarnedToday:      money(earnedToday.Int64),
		EarnedThisWeek:   money(earnedThisWeek.Int64),
		TotalEarned:      money(totalEarned.Int64),
		TotalPaidOut:     money(totalPaidOut.Int64),
		Balance:          money(totalEarned.Int64 - totalPaidOut.Int64),
	}
	if lastPayoutAt.Valid {
		t, err := parseTime(lastPayoutAt.String)
		if err != nil {
			return nil, err
		}
		summary.LastPayoutAt = timestampPB(t)
	}
	return summary, nil
}

func (s *Server) getUser(ctx context.Context, userID string) (*v1.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, family_id, name, role, created_at, email, auth_subject FROM users WHERE id = ?`,
		userID,
	)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return u, nil
}

func (s *Server) GetChildSummary(ctx context.Context, req *connect.Request[v1.GetChildSummaryRequest]) (*connect.Response[v1.GetChildSummaryResponse], error) {
	childID := req.Msg.GetChildId()
	if childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child_id is required"))
	}
	child, err := s.getUser(ctx, childID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMembership(ctx, child.FamilyId); err != nil {
		return nil, err
	}
	if err := s.requireSelfOrParent(ctx, child.FamilyId, childID); err != nil {
		return nil, err
	}
	summary, err := s.computeSummary(ctx, child)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetChildSummaryResponse{Summary: summary}), nil
}

func (s *Server) ListChildSummaries(ctx context.Context, req *connect.Request[v1.ListChildSummariesRequest]) (*connect.Response[v1.ListChildSummariesResponse], error) {
	familyID := s.resolvedFamilyID(ctx, req.Msg.GetFamilyId())
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if !s.authorizedForDashboard(ctx, familyID) {
		if err := s.requireMembership(ctx, familyID); err != nil {
			return nil, err
		}
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, "")
	if err != nil {
		return nil, err
	}
	query := `SELECT id, family_id, name, role, created_at, email, auth_subject FROM users WHERE family_id = ? AND role = 'child'`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND id = ?`
		args = append(args, childFilter)
	}
	query += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var children []*v1.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		children = append(children, u)
	}

	var summaries []*v1.ChildSummary
	for _, c := range children {
		summary, err := s.computeSummary(ctx, c)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		summaries = append(summaries, summary)
	}
	return connect.NewResponse(&v1.ListChildSummariesResponse{Summaries: summaries}), nil
}
