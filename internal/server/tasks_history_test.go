package server

import (
	"context"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

// historyFixture sets up one family, one child, and one daily task, with
// the given dates already completed.
type historyFixture struct {
	s        *Server
	ctx      context.Context
	familyID string
	childID  string
	taskID   string
}

func newHistoryFixture(t *testing.T, sub string, completed ...string) historyFixture {
	t.Helper()
	s := newTestServer(t)
	ctx := withIdentity(sub)

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	task, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(100), ChildIds: []string{child.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	f := historyFixture{s: s, ctx: ctx, familyID: fam.Msg.Family.Id, childID: child.Msg.User.Id, taskID: task.Msg.Task.Id}

	// A task is never due before it existed, so one created moments ago has
	// no past to test against. These fixtures work in 2024 dates, so the
	// task is made to have existed since just before then.
	f.backdateTo(t, "2023-12-31T00:00:00Z")

	for _, d := range completed {
		if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
			TaskId: task.Msg.Task.Id, ChildId: child.Msg.User.Id, DueDate: d,
		})); err != nil {
			t.Fatalf("CompleteTask %s: %v", d, err)
		}
	}
	return f
}

// backdate moves the task's creation date to n days ago, so it has history
// to have missed. A task can't be due before it existed, so a fixture whose
// task was created moments ago has no past occurrences to speak of.
func (f historyFixture) backdate(t *testing.T, days int) {
	t.Helper()
	f.backdateTo(t, formatTime(nowUTC().AddDate(0, 0, -days)))
}

func (f historyFixture) backdateTo(t *testing.T, createdAt string) {
	t.Helper()
	if _, err := f.s.db.Exec(`UPDATE tasks SET created_at = ? WHERE id = ?`, createdAt, f.taskID); err != nil {
		t.Fatalf("backdate task: %v", err)
	}
}

func (f historyFixture) summary(t *testing.T) *v1.ChildSummary {
	t.Helper()
	resp, err := f.s.GetChildSummary(f.ctx, connect.NewRequest(&v1.GetChildSummaryRequest{ChildId: f.childID}))
	if err != nil {
		t.Fatalf("GetChildSummary: %v", err)
	}
	return resp.Msg.Summary
}

// The bug this whole change exists to fix: deleting a task used to cascade
// its completions away, so a child who had already been paid out for that
// work was left with a negative balance.
func TestDeleteTask_KeepsEarningsAndBalance(t *testing.T) {
	f := newHistoryFixture(t, "auth0|delete-keeps-earnings", "2024-01-01", "2024-01-02", "2024-01-03")

	before := f.summary(t)
	if got := before.GetTotalEarned().GetCents(); got != 300 {
		t.Fatalf("expected 300 earned before deletion, got %d", got)
	}

	if _, err := f.s.CreatePayout(f.ctx, connect.NewRequest(&v1.CreatePayoutRequest{
		ChildId: f.childID, FullPayout: true,
	})); err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}
	if _, err := f.s.DeleteTask(f.ctx, connect.NewRequest(&v1.DeleteTaskRequest{TaskId: f.taskID})); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	after := f.summary(t)
	if got := after.GetTotalEarned().GetCents(); got != 300 {
		t.Errorf("earnings became %d after deleting the task, want 300 kept", got)
	}
	if got := after.GetTotalPaidOut().GetCents(); got != 300 {
		t.Errorf("paid out became %d, want 300", got)
	}
	// The specific symptom: paid-out survives the delete, so if earnings
	// don't, the balance goes negative.
	if got := after.GetBalance().GetCents(); got != 0 {
		t.Errorf("balance became %d after deleting a paid-out task, want 0", got)
	}
}

// Deleting a task keeps the occurrences it already produced — completed and
// merely due alike — and stops it producing any more.
func TestDeleteTask_KeepsPastOccurrencesAndStopsFuture(t *testing.T) {
	f := newHistoryFixture(t, "auth0|delete-keeps-occurrences", "2024-01-02")

	if _, err := f.s.DeleteTask(f.ctx, connect.NewRequest(&v1.DeleteTaskRequest{TaskId: f.taskID})); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}

	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: "2024-01-01", EndDate: "2024-01-03",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	byDate := map[string]*v1.TaskOccurrence{}
	for _, o := range resp.Msg.Occurrences {
		byDate[o.GetDueDate()] = o
	}
	// A daily task deleted today was due on all three of these dates.
	for _, d := range []string{"2024-01-01", "2024-01-02", "2024-01-03"} {
		if _, ok := byDate[d]; !ok {
			t.Fatalf("occurrence for %s disappeared when the task was deleted; got %+v", d, resp.Msg.Occurrences)
		}
	}
	if byDate["2024-01-02"].GetCompletedAt() == nil {
		t.Error("the completed occurrence lost its completion")
	}
	if byDate["2024-01-01"].GetCompletedAt() != nil {
		t.Error("an occurrence nobody completed came back marked complete")
	}
	// Each still says what the task said, with no task row left to join to.
	if got := byDate["2024-01-02"].GetTitle(); got != "Dishes" {
		t.Errorf("title = %q, want the snapshot to outlive the task", got)
	}

	// And the task itself is gone from the list a parent manages.
	tasks, err := f.s.ListTasks(f.ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: f.familyID}))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(tasks.Msg.Tasks) != 0 {
		t.Errorf("a deleted task is still listed: %+v", tasks.Msg.Tasks)
	}
	// It can't be edited or completed against any more, either.
	if _, err := f.s.CompleteTask(f.ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: f.taskID, ChildId: f.childID, DueDate: "2024-01-01",
	})); codeOf(err) != connect.CodeNotFound {
		t.Errorf("completing a deleted task returned %v, want NotFound", err)
	}
}

// Repricing a task must not revalue work already done — the amount is
// recorded when the occurrence is completed and never revisited.
func TestUpdateTask_DoesNotRepriceCompletedOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|reprice", "2024-01-01", "2024-01-02")

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(9900), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	if got := f.summary(t).GetTotalEarned().GetCents(); got != 200 {
		t.Errorf("earnings became %d after repricing 100 -> 9900, want the recorded 200", got)
	}

	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: "2024-01-01", EndDate: "2024-01-02",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	for _, o := range resp.Msg.Occurrences {
		if o.GetAmount().GetCents() != 100 {
			t.Errorf("occurrence on %s is worth %d, want the 100 it was completed at",
				o.GetDueDate(), o.GetAmount().GetCents())
		}
	}
}

// Renaming a task must not rewrite what history says it was called.
func TestUpdateTask_DoesNotRenameCompletedOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|rename", "2024-01-01")

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Load the dishwasher", Schedule: cronSchedule("0 0 * * *"),
		Price: money(100), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}

	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: "2024-01-01", EndDate: "2024-01-01",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	if len(resp.Msg.Occurrences) != 1 {
		t.Fatalf("expected exactly 1 occurrence, got %+v", resp.Msg.Occurrences)
	}
	if got := resp.Msg.Occurrences[0].GetTitle(); got != "Dishes" {
		t.Errorf("the completed occurrence is now titled %q, want the original \"Dishes\"", got)
	}
}

// task_occurrences holds rows for chores that were due and never done, and
// those carry an amount. Every earnings query has to exclude them, or a
// child is credited for work nobody did. This is the single most dangerous
// way this refactor could go wrong, so it gets its own test.
func TestChildSummary_ExcludesUncompletedOccurrences(t *testing.T) {
	f := newHistoryFixture(t, "auth0|uncompleted-not-earned", "2024-01-01")

	if got := f.summary(t).GetTotalEarned().GetCents(); got != 100 {
		t.Fatalf("expected exactly the one completed occurrence to count, got %d", got)
	}

	// A daily task since its creation has produced plenty of uncompleted
	// occurrences; confirm they're visible but worth nothing.
	resp, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: f.familyID, StartDate: "2024-01-01", EndDate: "2024-01-10",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	var uncompleted int
	for _, o := range resp.Msg.Occurrences {
		if o.GetCompletedAt() == nil {
			uncompleted++
		}
	}
	if uncompleted == 0 {
		t.Fatal("expected uncompleted occurrences to be listed at all")
	}
	if got := f.summary(t).GetBalance().GetCents(); got != 100 {
		t.Errorf("balance = %d with %d uncompleted occurrences present, want 100", got, uncompleted)
	}
}

// A repeated completion — a double tap, a retried request — must not
// revalue an occurrence that was already recorded at a different price.
func TestCompleteTask_RepeatedCompletionKeepsTheOriginalAmount(t *testing.T) {
	f := newHistoryFixture(t, "auth0|double-complete", "2024-01-01")

	if _, err := f.s.UpdateTask(f.ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: f.taskID, Title: "Dishes", Schedule: cronSchedule("0 0 * * *"),
		Price: money(5000), ChildIds: []string{f.childID}, Active: true,
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	resp, err := f.s.CompleteTask(f.ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: f.taskID, ChildId: f.childID, DueDate: "2024-01-01",
	}))
	if err != nil {
		t.Fatalf("CompleteTask again: %v", err)
	}
	if got := resp.Msg.Occurrence.GetAmount().GetCents(); got != 100 {
		t.Errorf("re-completing repriced the occurrence to %d, want the original 100", got)
	}
	if got := f.summary(t).GetTotalEarned().GetCents(); got != 100 {
		t.Errorf("earnings became %d after a repeat completion, want 100", got)
	}
}
