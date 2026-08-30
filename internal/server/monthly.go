package server

import (
	"context"
	"errors"
	"sort"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

func (s *Server) ListMonthlyEarnings(ctx context.Context, req *connect.Request[v1.ListMonthlyEarningsRequest]) (*connect.Response[v1.ListMonthlyEarningsResponse], error) {
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
	months, err := s.monthlyEarnings(ctx, childID, nowUTC())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.ListMonthlyEarningsResponse{Months: months}), nil
}

// monthlyEarnings is one child's completed earnings, one total per calendar
// month, most recent first. Like totalEarnedCents, it combines two sources
// that between them cover all of history regardless of whether a purge has
// run: whatever occurrence rows are still inside the retention window,
// summed live, plus whatever a purge already compacted out of it into
// child_monthly_earnings before deleting the rows it was summed from — see
// purgeExpiredOccurrences. A month can legitimately have both (the month
// straddling the retention cutoff), so the two sources are added together
// rather than one taking priority.
//
// The current and previous month are always present, even at zero, so a
// caller can read the first two entries as "this month" and "last month"
// without checking whether anything was actually earned in either.
func (s *Server) monthlyEarnings(ctx context.Context, childID string, now time.Time) ([]*v1.MonthlyEarning, error) {
	totals := map[string]int64{}

	live, err := s.db.QueryContext(ctx, `
		SELECT strftime('%Y-%m', due_date), SUM(amount_cents)
		FROM task_occurrences
		WHERE child_id = ? AND completed_at IS NOT NULL
		GROUP BY strftime('%Y-%m', due_date)
	`, childID)
	if err != nil {
		return nil, err
	}
	defer live.Close()
	for live.Next() {
		var yearMonth string
		var cents int64
		if err := live.Scan(&yearMonth, &cents); err != nil {
			return nil, err
		}
		totals[yearMonth] += cents
	}
	if err := live.Err(); err != nil {
		return nil, err
	}

	compacted, err := s.db.QueryContext(ctx,
		`SELECT year_month, earned_cents FROM child_monthly_earnings WHERE child_id = ?`,
		childID,
	)
	if err != nil {
		return nil, err
	}
	defer compacted.Close()
	for compacted.Next() {
		var yearMonth string
		var cents int64
		if err := compacted.Scan(&yearMonth, &cents); err != nil {
			return nil, err
		}
		totals[yearMonth] += cents
	}
	if err := compacted.Err(); err != nil {
		return nil, err
	}

	// Anchored on the first of the month before subtracting: AddDate on, say,
	// the 31st would overflow into the wrong month for any 30-day month.
	firstOfThisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	for _, yearMonth := range []string{
		firstOfThisMonth.Format("2006-01"),
		firstOfThisMonth.AddDate(0, -1, 0).Format("2006-01"),
	} {
		if _, ok := totals[yearMonth]; !ok {
			totals[yearMonth] = 0
		}
	}

	yearMonths := make([]string, 0, len(totals))
	for yearMonth := range totals {
		yearMonths = append(yearMonths, yearMonth)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(yearMonths)))

	months := make([]*v1.MonthlyEarning, len(yearMonths))
	for i, yearMonth := range yearMonths {
		months[i] = &v1.MonthlyEarning{YearMonth: yearMonth, Earned: money(totals[yearMonth])}
	}
	return months, nil
}
