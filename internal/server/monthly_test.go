package server

import (
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

// findMonth is a small lookup helper — the order of the slice is asserted
// separately, so tests that only care about one month's total don't also
// have to hardcode every other entry's position.
func findMonth(months []*v1.MonthlyEarning, yearMonth string) (*v1.MonthlyEarning, bool) {
	for _, m := range months {
		if m.GetYearMonth() == yearMonth {
			return m, true
		}
	}
	return nil, false
}

// However bare the history, the current and previous month must always be
// there — Balance's two summary rows read months[0]/months[1] and would
// otherwise have to special-case an empty result.
func TestListMonthlyEarnings_AlwaysIncludesThisAndLastMonth(t *testing.T) {
	f := newHistoryFixture(t, "auth0|monthly-empty")
	now := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)

	months, err := f.s.monthlyEarnings(f.ctx, f.childID, now)
	if err != nil {
		t.Fatalf("monthlyEarnings: %v", err)
	}
	if len(months) != 2 {
		t.Fatalf("expected exactly the 2 always-present months, got %+v", months)
	}
	if months[0].GetYearMonth() != "2024-03" || months[0].GetEarned().GetCents() != 0 {
		t.Errorf("months[0] = %+v, want this month (2024-03) at 0", months[0])
	}
	if months[1].GetYearMonth() != "2024-02" || months[1].GetEarned().GetCents() != 0 {
		t.Errorf("months[1] = %+v, want last month (2024-02) at 0", months[1])
	}
}

// The ordinary case: everything is still inside the retention window, so
// the whole answer comes from summing task_occurrences — no purge involved.
func TestListMonthlyEarnings_SumsCompletedOccurrencesByMonth(t *testing.T) {
	f := newHistoryFixture(t, "auth0|monthly-live")
	now := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)

	f.completeOn(t, "2024-03-01", 500) // this month
	f.completeOn(t, "2024-02-20", 300) // last month

	months, err := f.s.monthlyEarnings(f.ctx, f.childID, now)
	if err != nil {
		t.Fatalf("monthlyEarnings: %v", err)
	}
	if len(months) != 2 {
		t.Fatalf("expected 2 months, got %+v", months)
	}
	if got := months[0]; got.GetYearMonth() != "2024-03" || got.GetEarned().GetCents() != 500 {
		t.Errorf("months[0] = %+v, want 2024-03 at 500", got)
	}
	if got := months[1]; got.GetYearMonth() != "2024-02" || got.GetEarned().GetCents() != 300 {
		t.Errorf("months[1] = %+v, want 2024-02 at 300", got)
	}
}

// The case the compaction exists for: a month old enough that part of it —
// or all of it — has already been purged. The purge cutoff sits in the
// middle of January here (2024-03-15 minus 62 days is 2024-01-13), so
// January has one occurrence on each side of it: the earlier one is purged
// and compacted into child_monthly_earnings, the later one stays live in
// task_occurrences. The month's total must be both added together, not
// either one alone.
func TestListMonthlyEarnings_MergesLiveAndCompactedAcrossPurgeBoundary(t *testing.T) {
	f := newHistoryFixture(t, "auth0|monthly-straddle")
	now := time.Date(2024, 3, 15, 12, 0, 0, 0, time.UTC)
	if got := retentionCutoff(now); got != "2024-01-13" {
		t.Fatalf("test assumes a 2024-01-13 cutoff, got %s — recompute the fixture dates", got)
	}

	f.completeOn(t, "2024-01-05", 400) // before the cutoff: will be purged
	f.completeOn(t, "2024-01-20", 250) // after the cutoff: stays live

	if _, err := f.s.purgeExpiredOccurrences(f.ctx, now); err != nil {
		t.Fatalf("purge: %v", err)
	}

	months, err := f.s.monthlyEarnings(f.ctx, f.childID, now)
	if err != nil {
		t.Fatalf("monthlyEarnings: %v", err)
	}
	jan, ok := findMonth(months, "2024-01")
	if !ok {
		t.Fatalf("no 2024-01 entry in %+v", months)
	}
	if got := jan.GetEarned().GetCents(); got != 650 {
		t.Errorf("2024-01 earned = %d, want 400 (compacted) + 250 (live) = 650", got)
	}

	// A second purge must not double-count the part it already compacted.
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, now); err != nil {
		t.Fatalf("second purge: %v", err)
	}
	months, err = f.s.monthlyEarnings(f.ctx, f.childID, now)
	if err != nil {
		t.Fatalf("monthlyEarnings after second purge: %v", err)
	}
	jan, ok = findMonth(months, "2024-01")
	if !ok {
		t.Fatalf("no 2024-01 entry after second purge in %+v", months)
	}
	if got := jan.GetEarned().GetCents(); got != 650 {
		t.Errorf("2024-01 earned after a repeated purge = %d, want still 650", got)
	}
}

// A stranger to the family must not learn what a child in it earned.
func TestListMonthlyEarnings_RequiresMembership(t *testing.T) {
	f := newHistoryFixture(t, "auth0|monthly-owner")
	outsider := withIdentity("auth0|monthly-stranger")

	_, err := f.s.ListMonthlyEarnings(outsider, connect.NewRequest(&v1.ListMonthlyEarningsRequest{ChildId: f.childID}))
	if err == nil {
		t.Fatal("expected an error for a non-member, got none")
	}
}

// One child in a family must not be able to read another's earnings —
// the same restriction requireSelfOrParent gives GetChildSummary.
func TestListMonthlyEarnings_ChildCannotReadASiblingsEarnings(t *testing.T) {
	f := newHistoryFixture(t, "auth0|monthly-parent")
	sibling, err := f.s.CreateUser(f.ctx, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: f.familyID, Name: "Sibling", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Bind the sibling to their own login identity, the way an accepted
	// invitation would, so requireSelfOrParent treats them as that child
	// rather than as the still-unbound founding parent.
	if _, err := f.s.db.Exec(`UPDATE users SET auth_subject = ? WHERE id = ?`, "auth0|monthly-sibling", sibling.Msg.User.Id); err != nil {
		t.Fatalf("bind sibling: %v", err)
	}

	siblingCtx := withIdentity("auth0|monthly-sibling")
	if _, err := f.s.ListMonthlyEarnings(siblingCtx, connect.NewRequest(&v1.ListMonthlyEarningsRequest{ChildId: f.childID})); codeOf(err) != connect.CodePermissionDenied {
		t.Errorf("sibling reading another child's earnings: got %v, want PermissionDenied", err)
	}
}
