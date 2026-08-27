package server

import (
	"sync"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

// occurrencesByDate fetches the family's occurrences over a range, keyed by
// due date. Every fixture here uses a single task and a single child, so
// the date alone identifies an occurrence.
func (f historyFixture) occurrencesByDate(t *testing.T, start, end string) map[string]*v1.TaskOccurrence {
	t.Helper()
	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: start, EndDate: end,
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	byDate := map[string]*v1.TaskOccurrence{}
	for _, o := range resp.Msg.Occurrences {
		byDate[o.GetDueDate()] = o
	}
	return byDate
}

// The gap this closes: an occurrence nobody completed has no row of its
// own, so it renders from the task's current price. Repricing the task used
// to restate what last week's missed chores were worth. Editing now freezes
// everything already due first.
func TestUpdateTask_FreezesUncompletedPastOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|freeze-uncompleted")
	f.backdate(t, 30)
	yesterday := scheduling.FormatDate(nowUTC().AddDate(0, 0, -1))

	// Nothing has been completed, so yesterday exists only as a derivation.
	if got := f.occurrencesByDate(t, yesterday, yesterday)[yesterday]; got == nil {
		t.Fatalf("expected yesterday's occurrence to exist before the edit")
	} else if got.GetAmount().GetCents() != 100 {
		t.Fatalf("expected yesterday to be worth the task's 100, got %d", got.GetAmount().GetCents())
	}

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes, properly", Schedule: cronSchedule("0 0 * * *"),
		Price: money(4200), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	after := f.occurrencesByDate(t, yesterday, yesterday)[yesterday]
	if after == nil {
		t.Fatal("yesterday's occurrence disappeared after the edit")
	}
	if got := after.GetAmount().GetCents(); got != 100 {
		t.Errorf("yesterday is now worth %d; the edit restated an occurrence that had already come due", got)
	}
	// The title, by contrast, is read live and is *meant* to follow the
	// rename — it's a label, not money.
	if got := after.GetTitle(); got != "Dishes, properly" {
		t.Errorf("yesterday is titled %q; a rename should reach every occurrence", got)
	}

	// Today and later do track the edit — correcting a price this morning
	// should apply to this morning's chore.
	today := scheduling.FormatDate(nowUTC())
	if got := f.occurrencesByDate(t, today, today)[today]; got == nil {
		t.Fatal("today's occurrence went missing")
	} else if got.GetAmount().GetCents() != 4200 {
		t.Errorf("today is worth %d, want the new 4200", got.GetAmount().GetCents())
	}
}

// Freezing costs one row per past due date per assigned child, so it only
// runs when the edit would actually restate something. Pausing changes
// whether a task comes due from here on, not what it was worth or called
// last month — so it must write nothing, or pausing a long-running daily
// chore becomes a thousand-row write for no gain.
func TestUpdateTask_PausingDoesNotFreeze(t *testing.T) {
	f := newHistoryFixture(t, "auth0|pause-no-freeze")
	f.backdate(t, 30)

	countRows := func() int {
		t.Helper()
		var n int
		if err := f.s.db.QueryRow(
			`SELECT COUNT(*) FROM task_occurrences WHERE task_id = ?`, f.taskID).Scan(&n); err != nil {
			t.Fatalf("count occurrences: %v", err)
		}
		return n
	}
	if before := countRows(); before != 0 {
		t.Fatalf("expected no stored occurrences to start with, got %d", before)
	}

	// Same task, same everything, only paused.
	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(100), ChildIds: []string{f.childID}, Active: false,
	})); err != nil {
		t.Fatalf("UpdateTask pause: %v", err)
	}
	if after := countRows(); after != 0 {
		t.Errorf("pausing wrote %d occurrence rows, want 0", after)
	}

	// Resuming and then genuinely repricing does freeze.
	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(700), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask reprice: %v", err)
	}
	if after := countRows(); after == 0 {
		t.Error("repricing froze nothing; the task's past is still derived from its new price")
	}
}

// A frozen occurrence is still a real chore someone can tick off, and doing
// so must pay what it was frozen at rather than the task's current price.
func TestCompleteTask_CompletingAFrozenOccurrenceUsesTheFrozenAmount(t *testing.T) {
	f := newHistoryFixture(t, "auth0|complete-frozen")
	f.backdate(t, 30)
	yesterday := scheduling.FormatDate(nowUTC().AddDate(0, 0, -1))

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(4200), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	resp, err := f.s.CompleteTask(f.ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: f.taskID, ChildId: f.childID, DueDate: yesterday,
	}))
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if resp.Msg.Occurrence.GetCompletedAt() == nil {
		t.Fatal("completing a frozen occurrence left it uncompleted")
	}
	if got := resp.Msg.Occurrence.GetAmount().GetCents(); got != 100 {
		t.Errorf("paid %d for a chore frozen at 100 before the price rose to 4200", got)
	}
	if got := f.summary(t).GetTotalEarned().GetCents(); got != 100 {
		t.Errorf("earnings = %d, want 100", got)
	}
}

// Un-ticking a chore must not hand it back to the task's current price:
// tick, un-tick and tick again should pay the same each time.
func TestUncompleteTask_KeepsTheFrozenAmount(t *testing.T) {
	f := newHistoryFixture(t, "auth0|uncomplete-keeps", "2024-01-01")

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(4200), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if _, err := f.s.UncompleteTask(f.ctx, connect.NewRequest(&v1.UncompleteTaskRequest{
		TaskId: f.taskID, ChildId: f.childID, DueDate: "2024-01-01",
	})); err != nil {
		t.Fatalf("UncompleteTask: %v", err)
	}
	if got := f.summary(t).GetTotalEarned().GetCents(); got != 0 {
		t.Fatalf("earnings = %d after un-ticking the only completion, want 0", got)
	}

	resp, err := f.s.CompleteTask(f.ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: f.taskID, ChildId: f.childID, DueDate: "2024-01-01",
	}))
	if err != nil {
		t.Fatalf("CompleteTask again: %v", err)
	}
	if got := resp.Msg.Occurrence.GetAmount().GetCents(); got != 100 {
		t.Errorf("re-ticking paid %d, want the original 100 — un-ticking lost the frozen amount", got)
	}
}

// A payout can't exceed what the child is owed. The frontend has always
// enforced this; the server didn't, so the API allowed a parent to drive a
// balance negative — the same end state as the deletion bug, by a
// different route.
func TestCreatePayout_RejectsMoreThanTheBalance(t *testing.T) {
	f := newHistoryFixture(t, "auth0|overdraw", "2024-01-01")

	if _, err := f.s.CreatePayout(f.ctx, connect.NewRequest(&v1.CreatePayoutRequest{
		ChildId: f.childID, Amount: money(500),
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("paying out 500 against a balance of 100 returned %v, want InvalidArgument", err)
	}
	if got := f.summary(t).GetBalance().GetCents(); got != 100 {
		t.Errorf("balance = %d, want the untouched 100", got)
	}
}

// Two concurrent "pay the full balance" requests used to read the same
// figure and both insert, paying the child twice over. Reading and
// recording now happen in one transaction, so exactly one wins and the
// balance lands at zero rather than negative.
func TestCreatePayout_ConcurrentFullPayoutsCannotOverdraw(t *testing.T) {
	f := newHistoryFixture(t, "auth0|payout-race", "2024-01-01", "2024-01-02", "2024-01-03")

	const attempts = 6
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.s.CreatePayout(f.ctx, connect.NewRequest(&v1.CreatePayoutRequest{
				ChildId: f.childID, FullPayout: true,
			}))
		}(i)
	}
	wg.Wait()

	var succeeded int
	for _, err := range errs {
		if err == nil {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Errorf("%d of %d concurrent full payouts succeeded, want exactly 1", succeeded, attempts)
	}
	if got := f.summary(t).GetBalance().GetCents(); got != 0 {
		t.Errorf("balance = %d after concurrent full payouts, want 0 — negative means the child was paid twice", got)
	}
	if got := f.summary(t).GetTotalPaidOut().GetCents(); got != 300 {
		t.Errorf("paid out %d, want exactly the 300 earned", got)
	}
}

// The case that most justifies freezing, and the one that isn't about
// cosmetics: changing a schedule changes which past dates the task is
// derived as having been due on. Without a freeze, last month's Mondays
// simply cease to have existed.
func TestUpdateTask_ScheduleChangeKeepsPastOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|schedule-change")
	f.backdate(t, 30)

	before := f.occurrencesByDate(t, daysAgo(20), daysAgo(20))
	if len(before) != 1 {
		t.Fatalf("expected a daily task to be due 20 days ago, got %+v", before)
	}

	// Daily -> the 1st of the month only, which almost certainly does not
	// include that date.
	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 1 * *"),
		Price: money(100), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	after := f.occurrencesByDate(t, daysAgo(20), daysAgo(20))
	if len(after) != 1 {
		t.Fatalf("changing the schedule erased an occurrence that had already come due; got %+v", after)
	}
}

// And the same for reassignment: dropping a child must not retract every
// chore they were ever asked to do.
func TestUpdateTask_ReassignmentKeepsPastOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|reassign")
	f.backdate(t, 30)

	other, err := f.s.CreateUser(f.ctx, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: f.familyID, Name: "Sibling", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(100), ChildIds: []string{other.Msg.User.Id}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: daysAgo(20), EndDate: daysAgo(20), ChildId: f.childID,
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	if len(resp.Msg.Occurrences) != 1 {
		t.Fatalf("reassigning the task erased the original child's past occurrences; got %+v", resp.Msg.Occurrences)
	}
}
