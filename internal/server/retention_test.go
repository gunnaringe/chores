package server

import (
	"math/rand"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

// completeOn marks the fixture's task done on a given date, bypassing the
// RPC so a date outside the retention window can still be recorded — the
// point being to set up history that a purge will later have to account for.
func (f historyFixture) completeOn(t *testing.T, date string, amountCents int64) {
	t.Helper()
	if _, err := f.s.db.Exec(`
		INSERT INTO task_occurrences (id, task_id, child_id, family_id, due_date, amount_cents, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		newID(), f.taskID, f.childID, f.familyID, date, amountCents, date+"T18:00:00Z",
	); err != nil {
		t.Fatalf("seed completion on %s: %v", date, err)
	}
}

func (f historyFixture) occurrenceRowCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM task_occurrences WHERE family_id = ?`, f.familyID).Scan(&n); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return n
}

func daysAgo(n int) string {
	return scheduling.FormatDate(nowUTC().AddDate(0, 0, -n))
}

// The requirement behind retentionDays: whatever the date, the window
// covers this month and the whole of the previous one.
func TestRetention_WindowCoversThisMonthAndAllOfLast(t *testing.T) {
	for _, d := range []time.Time{
		time.Date(2024, 1, 31, 12, 0, 0, 0, time.UTC), // widest span: back to 1 December
		time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC),  // narrowest: 1 February is days away
		time.Date(2025, 8, 31, 12, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC),
	} {
		firstOfLastMonth := time.Date(d.Year(), d.Month()-1, 1, 0, 0, 0, 0, time.UTC)
		if got := retentionCutoff(d); got > scheduling.FormatDate(firstOfLastMonth) {
			t.Errorf("on %s the cutoff is %s, which cuts into last month (starts %s)",
				d.Format("2006-01-02"), got, scheduling.FormatDate(firstOfLastMonth))
		}
	}
}

// The whole reason the ledger exists. Purging the rows a balance was
// computed from, while keeping the payouts made against them, is the same
// failure that deleting a task used to cause.
func TestRetention_PurgeDoesNotMoveTheBalance(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-balance")

	// 300 earned well outside the window, 100 inside it.
	f.completeOn(t, daysAgo(200), 100)
	f.completeOn(t, daysAgo(150), 100)
	f.completeOn(t, daysAgo(100), 100)
	f.completeOn(t, daysAgo(10), 100)

	before := f.summary(t)
	if got := before.GetTotalEarned().GetCents(); got != 400 {
		t.Fatalf("expected 400 earned before the purge, got %d", got)
	}

	// Pay out the lot, so any lost earnings would show up as a negative
	// balance rather than merely a smaller one.
	if _, err := f.s.CreatePayout(f.ctx, connect.NewRequest(&v1.CreatePayoutRequest{
		ChildId: f.childID, FullPayout: true,
	})); err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}

	deleted, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC())
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if deleted != 3 {
		t.Errorf("purged %d rows, want the 3 outside the window", deleted)
	}

	after := f.summary(t)
	if got := after.GetTotalEarned().GetCents(); got != 400 {
		t.Errorf("total earned became %d after the purge, want 400 — the ledger did not carry it", got)
	}
	if got := after.GetTotalPaidOut().GetCents(); got != 400 {
		t.Errorf("paid out = %d, want 400", got)
	}
	if got := after.GetBalance().GetCents(); got != 0 {
		t.Errorf("balance = %d after purging paid-out earnings, want 0 (negative means the child now owes money)", got)
	}
}

// Storage is the point of the exercise: rows really do go.
func TestRetention_PurgeRemovesOutOfWindowRows(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-rows")
	for _, d := range []int{300, 200, 100, 63, 61, 5} {
		f.completeOn(t, daysAgo(d), 100)
	}
	if got := f.occurrenceRowCount(t); got != 6 {
		t.Fatalf("expected 6 seeded rows, got %d", got)
	}
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	// 61 and 5 days ago are inside a 62-day window; the rest are not.
	if got := f.occurrenceRowCount(t); got != 2 {
		t.Errorf("%d rows left after the purge, want the 2 inside the window", got)
	}
	if got := f.summary(t).GetTotalEarned().GetCents(); got != 600 {
		t.Errorf("total earned = %d, want all 600 still accounted for", got)
	}
}

// Nothing may depend on a purge having run — the machine sleeps, so it can
// be skipped or repeated arbitrarily.
func TestRetention_PurgeIsIdempotentAndOptional(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-idempotent")
	f.completeOn(t, daysAgo(300), 250)
	f.completeOn(t, daysAgo(3), 125)

	want := int64(375)
	if got := f.summary(t).GetTotalEarned().GetCents(); got != want {
		t.Fatalf("before any purge: %d, want %d", got, want)
	}
	for i := 1; i <= 4; i++ {
		if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
			t.Fatalf("purge #%d: %v", i, err)
		}
		if got := f.summary(t).GetTotalEarned().GetCents(); got != want {
			t.Fatalf("after purge #%d: %d, want %d — repeated purges must not double-count", i, got, want)
		}
	}
}

// The invariant stated in schema.sql, checked against a randomized sequence
// of the operations that touch it. This is the test that would catch a
// future change to the earnings queries forgetting the ledger.
func TestRetention_LedgerInvariantUnderRandomOperations(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-invariant")
	rng := rand.New(rand.NewSource(20260827))

	var expectedEarned int64
	// A row exists per date at most once (the unique key), and may be
	// completed or not — tracked separately, since un-completing leaves the
	// row behind.
	rowAmount := map[string]int64{}
	completed := map[string]bool{}

	for step := 0; step < 200; step++ {
		switch rng.Intn(4) {
		case 0, 1: // record a completion on a date with no row yet
			date := daysAgo(rng.Intn(365))
			if _, exists := rowAmount[date]; exists {
				continue
			}
			amount := int64((rng.Intn(20) + 1) * 25)
			f.completeOn(t, date, amount)
			rowAmount[date] = amount
			completed[date] = true
			expectedEarned += amount

		case 2: // un-complete one, which must remove its value but keep the row
			for date := range completed {
				if _, err := f.s.db.Exec(
					`UPDATE task_occurrences SET completed_at = NULL WHERE family_id = ? AND due_date = ?`,
					f.familyID, date); err != nil {
					t.Fatalf("uncomplete: %v", err)
				}
				expectedEarned -= rowAmount[date]
				delete(completed, date)
				break
			}

		case 3: // purge, which must move value rather than destroy it
			if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
				t.Fatalf("purge: %v", err)
			}
			cutoff := retentionCutoff(nowUTC())
			for date := range rowAmount {
				if date < cutoff {
					// The row is gone: its value now lives in the ledger if
					// it was completed, and is simply gone if it wasn't.
					delete(rowAmount, date)
					delete(completed, date)
				}
			}
		}

		got, err := f.s.totalEarnedCents(f.ctx, f.s.db, f.childID)
		if err != nil {
			t.Fatalf("step %d: %v", step, err)
		}
		if got != expectedEarned {
			t.Fatalf("step %d: total earned drifted to %d, want %d", step, got, expectedEarned)
		}
	}
	t.Logf("invariant held across 200 operations; final earned = %d", expectedEarned)
}

// Removing a child takes their ledger with them, or a re-added child could
// inherit a stranger's carried balance.
func TestRetention_LedgerCascadesWithTheChild(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-cascade")
	f.completeOn(t, daysAgo(200), 500)
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var ledgerRows int
	f.s.db.QueryRow(`SELECT COUNT(*) FROM child_ledger WHERE child_id = ?`, f.childID).Scan(&ledgerRows)
	if ledgerRows != 1 {
		t.Fatalf("expected the purge to write a ledger row, got %d", ledgerRows)
	}

	if _, err := f.s.db.Exec(`DELETE FROM users WHERE id = ?`, f.childID); err != nil {
		t.Fatalf("remove child: %v", err)
	}
	f.s.db.QueryRow(`SELECT COUNT(*) FROM child_ledger WHERE child_id = ?`, f.childID).Scan(&ledgerRows)
	if ledgerRows != 0 {
		t.Errorf("%d ledger rows survived the child's removal, want 0", ledgerRows)
	}
}

// child_monthly_earnings is keyed by child, same as child_ledger, so a
// removed child must take both with them or a re-added child could inherit
// a stranger's compacted earnings history.
func TestRetention_MonthlyEarningsCascadesWithTheChild(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-monthly-cascade")
	f.completeOn(t, daysAgo(200), 500)
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var rows int
	f.s.db.QueryRow(`SELECT COUNT(*) FROM child_monthly_earnings WHERE child_id = ?`, f.childID).Scan(&rows)
	if rows != 1 {
		t.Fatalf("expected the purge to write a monthly-earnings row, got %d", rows)
	}

	if _, err := f.s.db.Exec(`DELETE FROM users WHERE id = ?`, f.childID); err != nil {
		t.Fatalf("remove child: %v", err)
	}
	f.s.db.QueryRow(`SELECT COUNT(*) FROM child_monthly_earnings WHERE child_id = ?`, f.childID).Scan(&rows)
	if rows != 0 {
		t.Errorf("%d monthly-earnings rows survived the child's removal, want 0", rows)
	}
}

// A soft-deleted task is kept only so its occurrences can resolve a title.
// Once those have aged out it has no readers left, and the purge reclaims
// it — the storage argument for soft deletion having a bounded cost.
func TestRetention_PurgeReclaimsSoftDeletedTasks(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-tasks")

	if _, err := f.s.DeleteTask(f.ctx, connect.NewRequest(&v1.DeleteTaskRequest{TaskId: f.taskID})); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	taskRows := func() int {
		t.Helper()
		var n int
		if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE id = ?`, f.taskID).Scan(&n); err != nil {
			t.Fatalf("count tasks: %v", err)
		}
		return n
	}

	// Deleted just now, so still well inside the window: its occurrences
	// need it, and it must survive.
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if taskRows() != 1 {
		t.Fatal("a task deleted today was purged; its occurrences still need it to resolve a title")
	}

	// Once the deletion itself falls outside the window, every occurrence it
	// could have produced has gone with it.
	if _, err := f.s.db.Exec(`UPDATE tasks SET deleted_at = ? WHERE id = ?`,
		formatTime(nowUTC().AddDate(0, 0, -retentionDays-1)), f.taskID); err != nil {
		t.Fatalf("age the deletion: %v", err)
	}
	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if taskRows() != 0 {
		t.Error("a task deleted beyond the retention window was kept; nothing references it any more")
	}
}

// A live task is never reclaimed, however old.
func TestRetention_PurgeKeepsLiveTasks(t *testing.T) {
	f := newHistoryFixture(t, "auth0|purge-keeps-live")
	f.backdateTo(t, "2020-01-01T00:00:00Z")

	if _, err := f.s.purgeExpiredOccurrences(f.ctx, nowUTC()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	tasks, err := f.s.ListTasks(f.ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: f.familyID}))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.Msg.Tasks) != 1 {
		t.Errorf("a live task was purged: %+v", tasks.Msg.Tasks)
	}
}
