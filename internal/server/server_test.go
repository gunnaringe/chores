package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/db"
	"github.com/gunnaringe/chores/internal/scheduling"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	conn, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return New(conn)
}

func withIdentity(sub string) context.Context {
	return auth.NewContextWithIdentity(context.Background(), auth.Identity{Sub: sub, Name: "Test User " + sub, Email: sub + "@example.com"})
}

func codeOf(err error) connect.Code {
	return connect.CodeOf(err)
}

func TestCreateFamily_DisabledMode_DoesNotAutoBind(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background() // no identity: local-testing mode

	resp, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	users, err := s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: resp.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users.Msg.Users) != 0 {
		t.Fatalf("expected no auto-created user in disabled mode, got %d", len(users.Msg.Users))
	}
}

func TestCreateFamily_Auth0Mode_AutoBindsFoundingParent(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|mom")

	resp, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Mom"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	membership, err := s.GetMyMembership(ctx, connect.NewRequest(&v1.GetMyMembershipRequest{}))
	if err != nil {
		t.Fatalf("GetMyMembership: %v", err)
	}
	if !membership.Msg.Bound {
		t.Fatal("expected the founding parent to be bound")
	}
	if len(membership.Msg.Memberships) != 1 {
		t.Fatalf("expected exactly 1 membership, got %d", len(membership.Msg.Memberships))
	}
	m := membership.Msg.Memberships[0]
	if m.User.Name != "Mom" {
		t.Fatalf("expected bound user name %q, got %q", "Mom", m.User.Name)
	}
	if m.Family.Id != resp.Msg.Family.Id {
		t.Fatalf("bound family %q does not match created family %q", m.Family.Id, resp.Msg.Family.Id)
	}
	if !m.User.AuthBound {
		t.Fatal("expected AuthBound to be true for the founding parent")
	}

	// Founding a second family with the same identity must fail: one login
	// can only ever belong to one family.
	if _, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Second Family"})); err == nil {
		t.Fatal("expected an error creating a second family for an already-bound identity")
	} else if codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v", codeOf(err))
	}
}

func TestFamilyScoping_CrossFamilyAccessDenied(t *testing.T) {
	s := newTestServer(t)
	ctxA := withIdentity("auth0|parentA")
	ctxB := withIdentity("auth0|parentB")

	famA, err := s.CreateFamily(ctxA, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family A", ParentName: "Parent A"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famB, err := s.CreateFamily(ctxB, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family B", ParentName: "Parent B"}))
	if err != nil {
		t.Fatalf("CreateFamily B: %v", err)
	}

	// Parent A must not be able to list Family B's users, or vice versa.
	if _, err := s.ListUsers(ctxA, connect.NewRequest(&v1.ListUsersRequest{FamilyId: famB.Msg.Family.Id})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied listing another family's users, got %v", err)
	}
	if _, err := s.ListUsers(ctxB, connect.NewRequest(&v1.ListUsersRequest{FamilyId: famA.Msg.Family.Id})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied listing another family's users, got %v", err)
	}

	// Nor create a task in it.
	if _, err := s.CreateTask(ctxA, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famB.Msg.Family.Id, Title: "Sneaky task", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied creating a task in another family, got %v", err)
	}

	// ListFamilies must only ever show the caller's own family.
	familiesA, err := s.ListFamilies(ctxA, connect.NewRequest(&v1.ListFamiliesRequest{}))
	if err != nil {
		t.Fatalf("ListFamilies A: %v", err)
	}
	if len(familiesA.Msg.Families) != 1 || familiesA.Msg.Families[0].Id != famA.Msg.Family.Id {
		t.Fatalf("expected ListFamilies to return only Family A, got %+v", familiesA.Msg.Families)
	}

	// A task and a child created within Family A should be usable by Parent
	// A but not reachable from Family B's context.
	child, err := s.CreateUser(ctxA, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	task, err := s.CreateTask(ctxA, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famA.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{child.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	if _, err := s.CompleteTask(ctxB, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: child.Msg.User.Id, DueDate: "2024-01-01",
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied completing another family's task, got %v", err)
	}
}

func TestInvitationFlow(t *testing.T) {
	s := newTestServer(t)
	ctxParent := withIdentity("auth0|parent1")

	fam, err := s.CreateFamily(ctxParent, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	invResp, err := s.CreateInvitation(ctxParent, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Parent Two", Email: "p2@example.com", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if invResp.Msg.Token == "" {
		t.Fatal("expected a non-empty invitation token")
	}

	// A second, independent login accepts the invite.
	ctxParent2 := withIdentity("auth0|parent2")
	acceptResp, err := s.AcceptInvitation(ctxParent2, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invResp.Msg.Token}))
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if acceptResp.Msg.Family.Id != fam.Msg.Family.Id {
		t.Fatalf("expected accepted invitation to bind into family %q, got %q", fam.Msg.Family.Id, acceptResp.Msg.Family.Id)
	}
	if acceptResp.Msg.User.Name != "Parent Two" {
		t.Fatalf("expected bound user name %q, got %q", "Parent Two", acceptResp.Msg.User.Name)
	}

	// The now-bound second parent can operate within the shared family.
	if _, err := s.ListUsers(ctxParent2, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id})); err != nil {
		t.Fatalf("ListUsers as newly-bound parent: %v", err)
	}

	// The token is single-use.
	if _, err := s.AcceptInvitation(ctxParent2, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invResp.Msg.Token})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition re-accepting a used invitation, got %v", err)
	}

	// A third identity can't reuse the same (already-claimed) token either.
	ctxParent3 := withIdentity("auth0|parent3")
	if _, err := s.AcceptInvitation(ctxParent3, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invResp.Msg.Token})); err == nil {
		t.Fatal("expected an error accepting an already-used invitation from a different identity")
	}
}

func TestInvitationFlow_RevokeRemovesUnclaimedSlot(t *testing.T) {
	s := newTestServer(t)
	ctxParent := withIdentity("auth0|parent1")

	fam, err := s.CreateFamily(ctxParent, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	invResp, err := s.CreateInvitation(ctxParent, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Parent Two", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	if _, err := s.RevokeInvitation(ctxParent, connect.NewRequest(&v1.RevokeInvitationRequest{InvitationId: invResp.Msg.Invitation.Id})); err != nil {
		t.Fatalf("RevokeInvitation: %v", err)
	}

	users, err := s.ListUsers(ctxParent, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	for _, u := range users.Msg.Users {
		if u.Name == "Parent Two" {
			t.Fatal("expected the unclaimed placeholder user to be removed after revoking its invitation")
		}
	}

	// The revoked token must no longer work.
	ctxOther := withIdentity("auth0|someone-else")
	if _, err := s.AcceptInvitation(ctxOther, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invResp.Msg.Token})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected CodeNotFound accepting a revoked invitation, got %v", err)
	}
}

func TestChildInvitation_BindsAndRestrictsToSelf(t *testing.T) {
	s := newTestServer(t)
	ctxParent := withIdentity("auth0|parent1")

	fam, err := s.CreateFamily(ctxParent, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	invResp, err := s.CreateInvitation(ctxParent, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if invResp.Msg.Invitation.Role != v1.UserRole_USER_ROLE_CHILD {
		t.Fatalf("expected invitation role CHILD, got %v", invResp.Msg.Invitation.Role)
	}

	ctxChild := withIdentity("auth0|kid")
	acceptResp, err := s.AcceptInvitation(ctxChild, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invResp.Msg.Token}))
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if acceptResp.Msg.User.Role != v1.UserRole_USER_ROLE_CHILD {
		t.Fatalf("expected bound role CHILD, got %v", acceptResp.Msg.User.Role)
	}
	childID := acceptResp.Msg.User.Id

	// A logged-in child cannot perform parent-only actions.
	for name, err := range map[string]error{
		"CreateTask": func() error {
			_, err := s.CreateTask(ctxChild, connect.NewRequest(&v1.CreateTaskRequest{
				FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
			}))
			return err
		}(),
		"CreateUser": func() error {
			_, err := s.CreateUser(ctxChild, connect.NewRequest(&v1.CreateUserRequest{
				FamilyId: fam.Msg.Family.Id, Name: "Sibling", Role: v1.UserRole_USER_ROLE_CHILD,
			}))
			return err
		}(),
		"CreateInvitation": func() error {
			_, err := s.CreateInvitation(ctxChild, connect.NewRequest(&v1.CreateInvitationRequest{
				FamilyId: fam.Msg.Family.Id, Name: "Sneaky Parent", Role: v1.UserRole_USER_ROLE_PARENT,
			}))
			return err
		}(),
		"CreatePayout": func() error {
			_, err := s.CreatePayout(ctxChild, connect.NewRequest(&v1.CreatePayoutRequest{
				ChildId: childID, FullPayout: true,
			}))
			return err
		}(),
	} {
		if codeOf(err) != connect.CodePermissionDenied {
			t.Errorf("%s: expected PermissionDenied for a child identity, got %v", name, err)
		}
	}

	// But a child can complete their own task and view their own summary.
	task, err := s.CreateTask(ctxParent, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{childID},
	}))
	if err != nil {
		t.Fatalf("CreateTask (parent): %v", err)
	}
	if _, err := s.CompleteTask(ctxChild, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childID, DueDate: "2024-01-01",
	})); err != nil {
		t.Fatalf("CompleteTask as self: %v", err)
	}
	if _, err := s.GetChildSummary(ctxChild, connect.NewRequest(&v1.GetChildSummaryRequest{ChildId: childID})); err != nil {
		t.Fatalf("GetChildSummary as self: %v", err)
	}
}

func TestChild_CannotActOnBehalfOfSibling(t *testing.T) {
	s := newTestServer(t)
	ctxParent := withIdentity("auth0|parent1")

	fam, err := s.CreateFamily(ctxParent, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	invA, err := s.CreateInvitation(ctxParent, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Kid A", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation A: %v", err)
	}
	ctxChildA := withIdentity("auth0|kidA")
	acceptA, err := s.AcceptInvitation(ctxChildA, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invA.Msg.Token}))
	if err != nil {
		t.Fatalf("AcceptInvitation A: %v", err)
	}
	childAID := acceptA.Msg.User.Id

	childB, err := s.CreateUser(ctxParent, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Kid B", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	childBID := childB.Msg.User.Id

	task, err := s.CreateTask(ctxParent, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{childAID, childBID},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Child A cannot complete Child B's task, nor view Child B's summary.
	if _, err := s.CompleteTask(ctxChildA, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childBID, DueDate: "2024-01-01",
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied completing a sibling's task, got %v", err)
	}
	if _, err := s.GetChildSummary(ctxChildA, connect.NewRequest(&v1.GetChildSummaryRequest{ChildId: childBID})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied viewing a sibling's summary, got %v", err)
	}

	// ListChildSummaries and ListPayouts must be silently scoped to self for
	// a bound child, regardless of what's asked for.
	summaries, err := s.ListChildSummaries(ctxChildA, connect.NewRequest(&v1.ListChildSummariesRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListChildSummaries as child: %v", err)
	}
	if len(summaries.Msg.Summaries) != 1 || summaries.Msg.Summaries[0].Child.Id != childAID {
		t.Fatalf("expected ListChildSummaries to only return the caller's own summary, got %+v", summaries.Msg.Summaries)
	}

	if _, err := s.CreatePayout(ctxParent, connect.NewRequest(&v1.CreatePayoutRequest{ChildId: childAID, AmountCents: 100})); err != nil {
		t.Fatalf("CreatePayout to A: %v", err)
	}
	if _, err := s.CreatePayout(ctxParent, connect.NewRequest(&v1.CreatePayoutRequest{ChildId: childBID, AmountCents: 200})); err != nil {
		t.Fatalf("CreatePayout to B: %v", err)
	}
	payouts, err := s.ListPayouts(ctxChildA, connect.NewRequest(&v1.ListPayoutsRequest{FamilyId: fam.Msg.Family.Id, ChildId: childBID}))
	if err != nil {
		t.Fatalf("ListPayouts as child: %v", err)
	}
	for _, p := range payouts.Msg.Payouts {
		if p.ChildId != childAID {
			t.Fatalf("expected ListPayouts to only ever return the caller's own payouts even when a different child_id was requested, got %+v", p)
		}
	}
}

func TestTaskAssignment(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background() // local-testing mode: no identity, no role restrictions

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	childA, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid A", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	childB, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid B", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}

	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "No one", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
	})); err == nil {
		t.Fatal("expected an error creating a task with no assigned children")
	}

	task, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{childA.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got := task.Msg.Task.ChildIds; len(got) != 1 || got[0] != childA.Msg.User.Id {
		t.Fatalf("expected task assigned to [%s], got %v", childA.Msg.User.Id, got)
	}

	// Child B isn't assigned, so completing on their behalf must fail...
	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childB.Msg.User.Id, DueDate: "2024-01-01",
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument completing a task for an unassigned child, got %v", err)
	}
	// ...while child A, who is assigned, can.
	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childA.Msg.User.Id, DueDate: "2024-01-01",
	})); err != nil {
		t.Fatalf("CompleteTask for assigned child: %v", err)
	}

	// Occurrences are generated per assigned child: with both kids in the
	// family but the task assigned to only one, exactly one occurrence
	// should come back for today, for that child.
	today := scheduling.FormatDate(time.Now())
	occResp, err := s.ListTaskOccurrences(ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: fam.Msg.Family.Id, StartDate: today, EndDate: today,
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	if len(occResp.Msg.Occurrences) != 1 {
		t.Fatalf("expected exactly 1 occurrence, got %d: %+v", len(occResp.Msg.Occurrences), occResp.Msg.Occurrences)
	}
	if occResp.Msg.Occurrences[0].ChildId != childA.Msg.User.Id {
		t.Fatalf("expected the occurrence to be for child A, got %+v", occResp.Msg.Occurrences[0])
	}

	// Update can reassign the task to the other child.
	if _, err := s.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: task.Msg.Task.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100, Active: true,
		ChildIds: []string{childB.Msg.User.Id},
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	updated, err := s.ListTasks(ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(updated.Msg.Tasks) != 1 || len(updated.Msg.Tasks[0].ChildIds) != 1 || updated.Msg.Tasks[0].ChildIds[0] != childB.Msg.User.Id {
		t.Fatalf("expected task reassigned to child B only, got %+v", updated.Msg.Tasks[0])
	}
}

func TestTaskRepeatModes(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	childIDs := []string{child.Msg.User.Id}

	// A missing repeat_mode is rejected outright.
	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "No mode", PriceCents: 100, ChildIds: childIDs,
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument with no repeat_mode, got %v", err)
	}

	// ONCE: due only on its date, nowhere else.
	once, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Wash the car", PriceCents: 500, ChildIds: childIDs,
		RepeatMode: v1.RepeatMode_REPEAT_MODE_ONCE, StartDate: "2026-09-15",
	}))
	if err != nil {
		t.Fatalf("CreateTask ONCE: %v", err)
	}
	if once.Msg.Task.StartDate != "2026-09-15" {
		t.Fatalf("expected start_date to round-trip, got %q", once.Msg.Task.StartDate)
	}
	occ, err := s.ListTaskOccurrences(ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: fam.Msg.Family.Id, StartDate: "2026-09-01", EndDate: "2026-09-30",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	var onceDates []string
	for _, o := range occ.Msg.Occurrences {
		if o.Task.Id == once.Msg.Task.Id {
			onceDates = append(onceDates, o.DueDate)
		}
	}
	if len(onceDates) != 1 || onceDates[0] != "2026-09-15" {
		t.Fatalf("expected the ONCE task due exactly on 2026-09-15, got %v", onceDates)
	}

	// A ONCE task with no start_date is rejected.
	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "No date", PriceCents: 100, ChildIds: childIDs,
		RepeatMode: v1.RepeatMode_REPEAT_MODE_ONCE,
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for ONCE with no start_date, got %v", err)
	}

	// WEEKLY with an interval > 1: due on the anchor week and every 2nd week
	// after, not the weeks in between.
	weekly, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Take out recycling", PriceCents: 50, ChildIds: childIDs,
		RepeatMode: v1.RepeatMode_REPEAT_MODE_WEEKLY, DaysOfWeek: []int32{1}, RepeatIntervalWeeks: 2, StartDate: "2026-08-24",
	}))
	if err != nil {
		t.Fatalf("CreateTask WEEKLY: %v", err)
	}
	occ, err = s.ListTaskOccurrences(ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: fam.Msg.Family.Id, StartDate: "2026-08-24", EndDate: "2026-09-14",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	var weeklyDates []string
	for _, o := range occ.Msg.Occurrences {
		if o.Task.Id == weekly.Msg.Task.Id {
			weeklyDates = append(weeklyDates, o.DueDate)
		}
	}
	wantWeekly := []string{"2026-08-24", "2026-09-07"}
	if len(weeklyDates) != len(wantWeekly) {
		t.Fatalf("expected due dates %v, got %v", wantWeekly, weeklyDates)
	}
	for i, d := range weeklyDates {
		if d != wantWeekly[i] {
			t.Fatalf("expected due dates %v, got %v", wantWeekly, weeklyDates)
		}
	}

	// A WEEKLY task with no days selected is rejected.
	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "No days", PriceCents: 100, ChildIds: childIDs,
		RepeatMode: v1.RepeatMode_REPEAT_MODE_WEEKLY,
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for WEEKLY with no days_of_week, got %v", err)
	}

	// CRON: the raw expression is used as-is (existing behavior).
	cron, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Pay rent", PriceCents: 0, ChildIds: childIDs,
		RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 1 * *",
	}))
	if err != nil {
		t.Fatalf("CreateTask CRON: %v", err)
	}
	occ, err = s.ListTaskOccurrences(ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
		FamilyId: fam.Msg.Family.Id, StartDate: "2026-09-01", EndDate: "2026-09-30",
	}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences: %v", err)
	}
	var cronDates []string
	for _, o := range occ.Msg.Occurrences {
		if o.Task.Id == cron.Msg.Task.Id {
			cronDates = append(cronDates, o.DueDate)
		}
	}
	if len(cronDates) != 1 || cronDates[0] != "2026-09-01" {
		t.Fatalf("expected the CRON task due exactly on 2026-09-01, got %v", cronDates)
	}
}

func TestTaskIcon(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	task, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{child.Msg.User.Id}, Icon: &v1.Icon{Type: v1.IconType_ICON_TYPE_EMOJI, Value: "🧹"},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if got := task.Msg.Task.Icon; got.GetType() != v1.IconType_ICON_TYPE_EMOJI || got.GetValue() != "🧹" {
		t.Fatalf("expected emoji icon 🧹 on create, got %+v", got)
	}

	fetched, err := s.ListTasks(ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if got := fetched.Msg.Tasks[0].Icon; len(fetched.Msg.Tasks) != 1 || got.GetType() != v1.IconType_ICON_TYPE_EMOJI || got.GetValue() != "🧹" {
		t.Fatalf("expected icon 🧹 to persist, got %+v", fetched.Msg.Tasks[0])
	}

	// Reassigning to a Font Awesome icon must round-trip the type too, not
	// just the value.
	if _, err := s.UpdateTask(ctx, connect.NewRequest(&v1.UpdateTaskRequest{
		TaskId: task.Msg.Task.Id, Title: "Dishes", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100, Active: true,
		ChildIds: []string{child.Msg.User.Id}, Icon: &v1.Icon{Type: v1.IconType_ICON_TYPE_FONT_AWESOME, Value: "broom"},
	})); err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	updated, err := s.ListTasks(ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListTasks after update: %v", err)
	}
	if got := updated.Msg.Tasks[0].Icon; len(updated.Msg.Tasks) != 1 || got.GetType() != v1.IconType_ICON_TYPE_FONT_AWESOME || got.GetValue() != "broom" {
		t.Fatalf("expected icon updated to font-awesome:broom, got %+v", updated.Msg.Tasks[0])
	}

	// A value with no (valid) type is rejected rather than silently guessed at.
	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Ambiguous", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{child.Msg.User.Id}, Icon: &v1.Icon{Value: "broom"},
	})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for a value with no icon type, got %v", err)
	}

	// No icon at all is fine and comes back as nil, not a zero-value Icon.
	noIcon, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "No icon", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{child.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask with no icon: %v", err)
	}
	if noIcon.Msg.Task.Icon != nil {
		t.Fatalf("expected no icon, got %+v", noIcon.Msg.Task.Icon)
	}
}

// TestChild_CanBeMemberOfMultipleFamilies covers the split-household case:
// the same login (e.g. a child) can be bound to a family member row in more
// than one family, each independently scoped — completing a task, viewing
// a summary, etc. in one family must have no bearing on the other.
func TestChild_CanBeMemberOfMultipleFamilies(t *testing.T) {
	s := newTestServer(t)
	ctxParentA := withIdentity("auth0|parentA")
	ctxParentB := withIdentity("auth0|parentB")
	ctxKid := withIdentity("auth0|kid")

	famA, err := s.CreateFamily(ctxParentA, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family A", ParentName: "Parent A"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famB, err := s.CreateFamily(ctxParentB, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family B", ParentName: "Parent B"}))
	if err != nil {
		t.Fatalf("CreateFamily B: %v", err)
	}

	invA, err := s.CreateInvitation(ctxParentA, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation A: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxKid, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invA.Msg.Token})); err != nil {
		t.Fatalf("AcceptInvitation A: %v", err)
	}

	invB, err := s.CreateInvitation(ctxParentB, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famB.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation B: %v", err)
	}
	acceptB, err := s.AcceptInvitation(ctxKid, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invB.Msg.Token}))
	if err != nil {
		t.Fatalf("expected the same login to accept a second family's invitation, got: %v", err)
	}

	membership, err := s.GetMyMembership(ctxKid, connect.NewRequest(&v1.GetMyMembershipRequest{}))
	if err != nil {
		t.Fatalf("GetMyMembership: %v", err)
	}
	if len(membership.Msg.Memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d: %+v", len(membership.Msg.Memberships), membership.Msg.Memberships)
	}
	seenFamilies := map[string]bool{}
	for _, m := range membership.Msg.Memberships {
		seenFamilies[m.Family.Id] = true
	}
	if !seenFamilies[famA.Msg.Family.Id] || !seenFamilies[famB.Msg.Family.Id] {
		t.Fatalf("expected memberships in both families, got %+v", membership.Msg.Memberships)
	}

	// Accepting the same family's invitation twice (a second, separate
	// invite to family A) must still be rejected.
	invA2, err := s.CreateInvitation(ctxParentA, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Kid Again", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation A2: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxKid, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invA2.Msg.Token})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected FailedPrecondition re-joining the same family, got %v", err)
	}

	// The two family memberships are fully independent: a task and
	// completion in family A must not be visible or actionable from B.
	taskA, err := s.CreateTask(ctxParentA, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famA.Msg.Family.Id, Title: "Chore A", Schedule: "0 0 * * *", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, PriceCents: 100,
		ChildIds: []string{membership.Msg.Memberships[0].User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	kidBUserID := acceptB.Msg.User.Id
	if _, err := s.CompleteTask(ctxKid, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: taskA.Msg.Task.Id, ChildId: kidBUserID, DueDate: "2024-01-01",
	})); err == nil {
		t.Fatal("expected completing family A's task using the family B user id to fail")
	}
}
