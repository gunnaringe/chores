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

func TestUpdateFamily_ParentOnly(t *testing.T) {
	s := newTestServer(t)
	ctxParent := withIdentity("auth0|parent1")

	fam, err := s.CreateFamily(ctxParent, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}

	resp, err := s.UpdateFamily(ctxParent, connect.NewRequest(&v1.UpdateFamilyRequest{FamilyId: fam.Msg.Family.Id, Name: "The Renamed Sons"}))
	if err != nil {
		t.Fatalf("UpdateFamily: %v", err)
	}
	if resp.Msg.Family.Name != "The Renamed Sons" {
		t.Fatalf("expected renamed family, got %q", resp.Msg.Family.Name)
	}

	ctxOutsider := withIdentity("auth0|outsider")
	if _, err := s.UpdateFamily(ctxOutsider, connect.NewRequest(&v1.UpdateFamilyRequest{FamilyId: fam.Msg.Family.Id, Name: "Hijacked"})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied from a non-member, got %v", err)
	}
}

func TestUpdateUser_SelfOnly(t *testing.T) {
	s := newTestServer(t)
	ctxParent1 := withIdentity("auth0|parent1")
	ctxParent2 := withIdentity("auth0|parent2")

	fam, err := s.CreateFamily(ctxParent1, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent One"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	inv, err := s.CreateInvitation(ctxParent1, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Parent Two", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxParent2, connect.NewRequest(&v1.AcceptInvitationRequest{Token: inv.Msg.Token})); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	users, err := s.ListUsers(ctxParent1, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	var p1ID, p2ID string
	for _, u := range users.Msg.Users {
		if u.Name == "Parent One" {
			p1ID = u.Id
		} else if u.Name == "Parent Two" {
			p2ID = u.Id
		}
	}
	if p1ID == "" || p2ID == "" {
		t.Fatalf("expected to find both parents, got %+v", users.Msg.Users)
	}

	// Renaming yourself succeeds.
	renamed, err := s.UpdateUser(ctxParent1, connect.NewRequest(&v1.UpdateUserRequest{UserId: p1ID, Name: "Parent One Renamed"}))
	if err != nil {
		t.Fatalf("UpdateUser self: %v", err)
	}
	if renamed.Msg.User.Name != "Parent One Renamed" {
		t.Fatalf("expected renamed name, got %q", renamed.Msg.User.Name)
	}

	// Renaming the other parent is rejected, even though both are parents in
	// the same family.
	if _, err := s.UpdateUser(ctxParent1, connect.NewRequest(&v1.UpdateUserRequest{UserId: p2ID, Name: "Hijacked"})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied renaming another parent, got %v", err)
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

// TestParent_CanBeMemberOfMultipleFamilies covers a parent co-running two
// households: unlike the old rule, accepting a second family's parent
// invitation must succeed, and only re-joining the *same* family is
// rejected.
func TestParent_CanBeMemberOfMultipleFamilies(t *testing.T) {
	s := newTestServer(t)
	ctxParentA := withIdentity("auth0|parentA")
	ctxParentB := withIdentity("auth0|parentB")
	ctxDad := withIdentity("auth0|dad")

	famA, err := s.CreateFamily(ctxParentA, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family A", ParentName: "Parent A"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famB, err := s.CreateFamily(ctxParentB, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family B", ParentName: "Parent B"}))
	if err != nil {
		t.Fatalf("CreateFamily B: %v", err)
	}

	invA, err := s.CreateInvitation(ctxParentA, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Dad", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation A: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxDad, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invA.Msg.Token})); err != nil {
		t.Fatalf("AcceptInvitation A: %v", err)
	}

	invB, err := s.CreateInvitation(ctxParentB, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famB.Msg.Family.Id, Name: "Dad", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation B: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxDad, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invB.Msg.Token})); err != nil {
		t.Fatalf("expected the same parent login to accept a second family's parent invitation, got: %v", err)
	}

	membership, err := s.GetMyMembership(ctxDad, connect.NewRequest(&v1.GetMyMembershipRequest{}))
	if err != nil {
		t.Fatalf("GetMyMembership: %v", err)
	}
	if len(membership.Msg.Memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d: %+v", len(membership.Msg.Memberships), membership.Msg.Memberships)
	}

	// Re-joining the same family (a second, separate invite to family A) must
	// still be rejected.
	invA2, err := s.CreateInvitation(ctxParentA, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Dad Again", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation A2: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxDad, connect.NewRequest(&v1.AcceptInvitationRequest{Token: invA2.Msg.Token})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected FailedPrecondition re-joining the same family, got %v", err)
	}
}

func TestChildSummary_EarnedThisWeek(t *testing.T) {
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
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{child.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	monday := mondayOfWeek(time.Now())
	inWeek := scheduling.FormatDate(monday)
	beforeWeek := scheduling.FormatDate(monday.AddDate(0, 0, -1)) // last Sunday: the prior week

	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: child.Msg.User.Id, DueDate: inWeek,
	})); err != nil {
		t.Fatalf("CompleteTask (in week): %v", err)
	}
	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: child.Msg.User.Id, DueDate: beforeWeek,
	})); err != nil {
		t.Fatalf("CompleteTask (before week): %v", err)
	}

	resp, err := s.GetChildSummary(ctx, connect.NewRequest(&v1.GetChildSummaryRequest{ChildId: child.Msg.User.Id}))
	if err != nil {
		t.Fatalf("GetChildSummary: %v", err)
	}
	if got := resp.Msg.Summary.EarnedThisWeekCents; got != 100 {
		t.Fatalf("expected earned_this_week_cents to count only this Monday's completion (100), got %d", got)
	}
	if got := resp.Msg.Summary.TotalEarnedCents; got != 200 {
		t.Fatalf("expected total_earned_cents to count both completions (200), got %d", got)
	}
}

func TestListTaskCompletions_SearchAndPagination(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	childA, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Alice", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	childB, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Bob", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	dishes, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{childA.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask Dishes: %v", err)
	}
	laundry, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Laundry", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 200, ChildIds: []string{childB.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask Laundry: %v", err)
	}

	dates := []string{"2024-01-01", "2024-01-02", "2024-01-03"}
	for _, d := range dates {
		if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
			TaskId: dishes.Msg.Task.Id, ChildId: childA.Msg.User.Id, DueDate: d,
		})); err != nil {
			t.Fatalf("CompleteTask Dishes %s: %v", d, err)
		}
	}
	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: laundry.Msg.Task.Id, ChildId: childB.Msg.User.Id, DueDate: "2024-01-01",
	})); err != nil {
		t.Fatalf("CompleteTask Laundry: %v", err)
	}

	// Search by task title (case-insensitive substring).
	resp, err := s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{FamilyId: fam.Msg.Family.Id, Search: "dish"}))
	if err != nil {
		t.Fatalf("ListTaskCompletions search by title: %v", err)
	}
	if len(resp.Msg.Completions) != 3 {
		t.Fatalf("expected 3 Dishes completions matching %q, got %d", "dish", len(resp.Msg.Completions))
	}
	for _, c := range resp.Msg.Completions {
		if c.TaskTitle != "Dishes" || c.ChildName != "Alice" {
			t.Fatalf("expected denormalized task_title/child_name on results, got %+v", c)
		}
	}

	// Search by child name.
	resp, err = s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{FamilyId: fam.Msg.Family.Id, Search: "bob"}))
	if err != nil {
		t.Fatalf("ListTaskCompletions search by child name: %v", err)
	}
	if len(resp.Msg.Completions) != 1 || resp.Msg.Completions[0].TaskTitle != "Laundry" {
		t.Fatalf("expected exactly the Laundry completion for Bob, got %+v", resp.Msg.Completions)
	}

	// Pagination over the 3 Dishes completions (ordered by due_date DESC):
	// a page of 2 reports has_more, and the next page picks up the rest.
	page1, err := s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{
		FamilyId: fam.Msg.Family.Id, ChildId: childA.Msg.User.Id, Limit: 2, Offset: 0,
	}))
	if err != nil {
		t.Fatalf("ListTaskCompletions page 1: %v", err)
	}
	if len(page1.Msg.Completions) != 2 || !page1.Msg.HasMore {
		t.Fatalf("expected page 1 to have 2 results and has_more=true, got %d results has_more=%v", len(page1.Msg.Completions), page1.Msg.HasMore)
	}
	if page1.Msg.Completions[0].DueDate != "2024-01-03" || page1.Msg.Completions[1].DueDate != "2024-01-02" {
		t.Fatalf("expected page 1 in due_date DESC order, got %+v", page1.Msg.Completions)
	}

	page2, err := s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{
		FamilyId: fam.Msg.Family.Id, ChildId: childA.Msg.User.Id, Limit: 2, Offset: 2,
	}))
	if err != nil {
		t.Fatalf("ListTaskCompletions page 2: %v", err)
	}
	if len(page2.Msg.Completions) != 1 || page2.Msg.HasMore {
		t.Fatalf("expected page 2 to have the last result and has_more=false, got %d results has_more=%v", len(page2.Msg.Completions), page2.Msg.HasMore)
	}
	if page2.Msg.Completions[0].DueDate != "2024-01-01" {
		t.Fatalf("expected the last remaining completion to be 2024-01-01, got %+v", page2.Msg.Completions[0])
	}

	// start_date/end_date bound the range (used for the "recent" bucket:
	// today/yesterday/this week).
	ranged, err := s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{
		FamilyId: fam.Msg.Family.Id, StartDate: "2024-01-02", EndDate: "2024-01-03",
	}))
	if err != nil {
		t.Fatalf("ListTaskCompletions ranged: %v", err)
	}
	if len(ranged.Msg.Completions) != 2 {
		t.Fatalf("expected 2 completions in [2024-01-02, 2024-01-03], got %d: %+v", len(ranged.Msg.Completions), ranged.Msg.Completions)
	}
}

func TestLeaveFamily_BlocksTheLastParent(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	parent, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	if _, err := s.LeaveFamily(ctx, connect.NewRequest(&v1.LeaveFamilyRequest{UserId: parent.Msg.User.Id})); codeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected FailedPrecondition leaving as the last parent, got %v", err)
	}

	// Still there afterward — the rejected attempt didn't partially apply.
	users, err := s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users.Msg.Users) != 1 {
		t.Fatalf("expected the last parent to still be a member, got %+v", users.Msg.Users)
	}

	// A second parent joining unblocks leaving for the first.
	dad, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Dad", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser Dad: %v", err)
	}
	if _, err := s.LeaveFamily(ctx, connect.NewRequest(&v1.LeaveFamilyRequest{UserId: parent.Msg.User.Id})); err != nil {
		t.Fatalf("LeaveFamily with a second parent present: %v", err)
	}
	users, err = s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users.Msg.Users) != 1 || users.Msg.Users[0].Id != dad.Msg.User.Id {
		t.Fatalf("expected only Dad to remain, got %+v", users.Msg.Users)
	}
}

func TestLeaveFamily_OnlyAsYourself(t *testing.T) {
	s := newTestServer(t)
	ctxMom := withIdentity("auth0|mom")
	ctxDad := withIdentity("auth0|dad")

	fam, err := s.CreateFamily(ctxMom, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Mom"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	inv, err := s.CreateInvitation(ctxMom, connect.NewRequest(&v1.CreateInvitationRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Dad", Role: v1.UserRole_USER_ROLE_PARENT,
	}))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if _, err := s.AcceptInvitation(ctxDad, connect.NewRequest(&v1.AcceptInvitationRequest{Token: inv.Msg.Token})); err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}

	membership, err := s.GetMyMembership(ctxMom, connect.NewRequest(&v1.GetMyMembershipRequest{}))
	if err != nil {
		t.Fatalf("GetMyMembership: %v", err)
	}
	momUserID := membership.Msg.Memberships[0].User.Id

	dadMembership, err := s.GetMyMembership(ctxDad, connect.NewRequest(&v1.GetMyMembershipRequest{}))
	if err != nil {
		t.Fatalf("GetMyMembership (dad): %v", err)
	}
	dadUserID := dadMembership.Msg.Memberships[0].User.Id

	// Mom cannot make Dad leave by calling LeaveFamily with his user id.
	if _, err := s.LeaveFamily(ctxMom, connect.NewRequest(&v1.LeaveFamilyRequest{UserId: dadUserID})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied leaving on another parent's behalf, got %v", err)
	}
	// Mom leaving as herself is fine (Dad remains as the other parent).
	if _, err := s.LeaveFamily(ctxMom, connect.NewRequest(&v1.LeaveFamilyRequest{UserId: momUserID})); err != nil {
		t.Fatalf("LeaveFamily as yourself: %v", err)
	}
}

func TestLeaveFamily_RejectsAChildTarget(t *testing.T) {
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
	if _, err := s.LeaveFamily(ctx, connect.NewRequest(&v1.LeaveFamilyRequest{UserId: child.Msg.User.Id})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument leaving as a child, got %v", err)
	}
}

func TestRemoveChild_CascadesAssignmentsAndHistory(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	parent, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser child: %v", err)
	}
	task, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{child.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if _, err := s.CompleteTask(ctx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: child.Msg.User.Id, DueDate: "2024-01-01",
	})); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if _, err := s.CreatePayout(ctx, connect.NewRequest(&v1.CreatePayoutRequest{ChildId: child.Msg.User.Id, FullPayout: true})); err != nil {
		t.Fatalf("CreatePayout: %v", err)
	}

	// A parent id is rejected outright.
	if _, err := s.RemoveChild(ctx, connect.NewRequest(&v1.RemoveChildRequest{ChildId: parent.Msg.User.Id})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument removing a parent as a child, got %v", err)
	}

	if _, err := s.RemoveChild(ctx, connect.NewRequest(&v1.RemoveChildRequest{ChildId: child.Msg.User.Id})); err != nil {
		t.Fatalf("RemoveChild: %v", err)
	}

	users, err := s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users.Msg.Users) != 1 || users.Msg.Users[0].Id != parent.Msg.User.Id {
		t.Fatalf("expected only the parent to remain, got %+v", users.Msg.Users)
	}
	updatedTask, err := s.ListTasks(ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListTasks: %v", err)
	}
	if len(updatedTask.Msg.Tasks) != 1 || len(updatedTask.Msg.Tasks[0].ChildIds) != 0 {
		t.Fatalf("expected the removed child's assignment to be gone, got %+v", updatedTask.Msg.Tasks[0].ChildIds)
	}
	completions, err := s.ListTaskCompletions(ctx, connect.NewRequest(&v1.ListTaskCompletionsRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListTaskCompletions: %v", err)
	}
	if len(completions.Msg.Completions) != 0 {
		t.Fatalf("expected the removed child's completion history to be gone, got %+v", completions.Msg.Completions)
	}
	payouts, err := s.ListPayouts(ctx, connect.NewRequest(&v1.ListPayoutsRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("ListPayouts: %v", err)
	}
	if len(payouts.Msg.Payouts) != 0 {
		t.Fatalf("expected the removed child's payout history to be gone, got %+v", payouts.Msg.Payouts)
	}
}

func TestDeleteFamily_RemovesEverything(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	if _, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT})); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser child: %v", err)
	}
	if _, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{child.Msg.User.Id},
	})); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if _, err := s.DeleteFamily(ctx, connect.NewRequest(&v1.DeleteFamilyRequest{FamilyId: fam.Msg.Family.Id})); err != nil {
		t.Fatalf("DeleteFamily: %v", err)
	}

	families, err := s.ListFamilies(ctx, connect.NewRequest(&v1.ListFamiliesRequest{}))
	if err != nil {
		t.Fatalf("ListFamilies: %v", err)
	}
	for _, f := range families.Msg.Families {
		if f.Id == fam.Msg.Family.Id {
			t.Fatalf("expected the deleted family to be gone from ListFamilies, got %+v", families.Msg.Families)
		}
	}

	// Deleting it again is a clean NotFound, not a silent no-op.
	if _, err := s.DeleteFamily(ctx, connect.NewRequest(&v1.DeleteFamilyRequest{FamilyId: fam.Msg.Family.Id})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected NotFound deleting an already-deleted family, got %v", err)
	}
}

// dashboardTestContext resolves key the same way DashboardOrAuth does over
// HTTP and returns a context carrying that resolution, for tests that need
// to exercise dashboard-authorized RPC calls without a real HTTP round trip.
func dashboardTestContext(t *testing.T, s *Server, key string) context.Context {
	t.Helper()
	familyID, ok := s.familyIDForDashboardKey(key)
	if !ok {
		t.Fatalf("dashboard key %q did not resolve to a family", key)
	}
	return newContextWithDashboardFamily(context.Background(), familyID)
}

func TestDashboardKey_ReadsAndCompletesTasksForTheWholeFamily(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	childA, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Alice", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
	}
	childB, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Bob", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	task, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{childA.Msg.User.Id, childB.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	setup, err := s.SetupDashboard(ctx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("SetupDashboard: %v", err)
	}
	if setup.Msg.DashboardKey == "" {
		t.Fatal("expected a non-empty dashboard key")
	}
	cfg, err := s.GetDashboardConfig(ctx, connect.NewRequest(&v1.GetDashboardConfigRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("GetDashboardConfig: %v", err)
	}
	if !cfg.Msg.Enabled || cfg.Msg.DashboardKey != setup.Msg.DashboardKey {
		t.Fatalf("expected GetDashboardConfig to report the key just set up, got %+v", cfg.Msg)
	}

	dashCtx := dashboardTestContext(t, s, setup.Msg.DashboardKey)

	// Sees every child's summary, not filtered to one — the request
	// deliberately carries no family_id at all, since a dashboard key
	// resolves it server-side.
	summaries, err := s.ListChildSummaries(dashCtx, connect.NewRequest(&v1.ListChildSummariesRequest{}))
	if err != nil {
		t.Fatalf("ListChildSummaries via dashboard: %v", err)
	}
	if len(summaries.Msg.Summaries) != 2 {
		t.Fatalf("expected summaries for both children, got %+v", summaries.Msg.Summaries)
	}

	today := scheduling.FormatDate(time.Now())
	occ, err := s.ListTaskOccurrences(dashCtx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{StartDate: today, EndDate: today}))
	if err != nil {
		t.Fatalf("ListTaskOccurrences via dashboard: %v", err)
	}
	if len(occ.Msg.Occurrences) != 2 {
		t.Fatalf("expected one occurrence per assigned child, got %+v", occ.Msg.Occurrences)
	}

	// Can mark either child's task done and undo it, like a parent can.
	if _, err := s.CompleteTask(dashCtx, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childA.Msg.User.Id, DueDate: today,
	})); err != nil {
		t.Fatalf("CompleteTask via dashboard: %v", err)
	}
	if _, err := s.UncompleteTask(dashCtx, connect.NewRequest(&v1.UncompleteTaskRequest{
		TaskId: task.Msg.Task.Id, ChildId: childA.Msg.User.Id, DueDate: today,
	})); err != nil {
		t.Fatalf("UncompleteTask via dashboard: %v", err)
	}

	// A wrong key resolves to nothing.
	if _, ok := s.familyIDForDashboardKey("not-a-real-key"); ok {
		t.Fatal("expected an unknown dashboard key to not resolve")
	}
}

// TestDashboardKey_NeverUnlocksParentOnlyActions is the critical security
// property of the whole feature: a context carrying nothing but dashboard
// authorization for a family must not be able to do anything beyond what
// the Today dashboard itself does, even when the RPC is called directly
// (as ListTaskOccurrences's own nested ListTasks/ListUsers calls do,
// bypassing the HTTP-layer allowlist that's the outer defense).
func TestDashboardKey_NeverUnlocksParentOnlyActions(t *testing.T) {
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
	setup, err := s.SetupDashboard(ctx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("SetupDashboard: %v", err)
	}
	dashCtx := dashboardTestContext(t, s, setup.Msg.DashboardKey)

	if _, err := s.CreateTask(dashCtx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: fam.Msg.Family.Id, Title: "Sneaky task", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *", PriceCents: 100,
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied creating a task via dashboard access, got %v", err)
	}
	if _, err := s.CreateUser(dashCtx, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: fam.Msg.Family.Id, Name: "Intruder", Role: v1.UserRole_USER_ROLE_PARENT,
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied adding a family member via dashboard access, got %v", err)
	}
	if _, err := s.CreatePayout(dashCtx, connect.NewRequest(&v1.CreatePayoutRequest{
		ChildId: child.Msg.User.Id, FullPayout: true,
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied creating a payout via dashboard access, got %v", err)
	}
	if _, err := s.DeleteFamily(dashCtx, connect.NewRequest(&v1.DeleteFamilyRequest{FamilyId: fam.Msg.Family.Id})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied deleting the family via dashboard access, got %v", err)
	}
	if _, err := s.SetupDashboard(dashCtx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: fam.Msg.Family.Id})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied regenerating the dashboard key via dashboard access itself, got %v", err)
	}
}

func TestDashboardKey_ScopedToItsOwnFamily(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	famA, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family A"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famB, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family B"}))
	if err != nil {
		t.Fatalf("CreateFamily B: %v", err)
	}
	childB, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: famB.Msg.Family.Id, Name: "Kid B", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser B: %v", err)
	}
	taskB, err := s.CreateTask(ctx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famB.Msg.Family.Id, Title: "Chore B", RepeatMode: v1.RepeatMode_REPEAT_MODE_CRON, Schedule: "0 0 * * *",
		PriceCents: 100, ChildIds: []string{childB.Msg.User.Id},
	}))
	if err != nil {
		t.Fatalf("CreateTask B: %v", err)
	}

	setupA, err := s.SetupDashboard(ctx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: famA.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("SetupDashboard A: %v", err)
	}
	dashCtxA := dashboardTestContext(t, s, setupA.Msg.DashboardKey)

	// Family A's dashboard key can't be used to complete family B's task.
	if _, err := s.CompleteTask(dashCtxA, connect.NewRequest(&v1.CompleteTaskRequest{
		TaskId: taskB.Msg.Task.Id, ChildId: childB.Msg.User.Id, DueDate: "2024-01-01",
	})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied completing another family's task via a mismatched dashboard key, got %v", err)
	}
}

func TestDisableDashboard_RevokesTheKey(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	setup, err := s.SetupDashboard(ctx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("SetupDashboard: %v", err)
	}
	if _, err := s.DisableDashboard(ctx, connect.NewRequest(&v1.DisableDashboardRequest{FamilyId: fam.Msg.Family.Id})); err != nil {
		t.Fatalf("DisableDashboard: %v", err)
	}
	if _, ok := s.familyIDForDashboardKey(setup.Msg.DashboardKey); ok {
		t.Fatal("expected the key to stop resolving after DisableDashboard")
	}
	cfg, err := s.GetDashboardConfig(ctx, connect.NewRequest(&v1.GetDashboardConfigRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("GetDashboardConfig: %v", err)
	}
	if cfg.Msg.Enabled {
		t.Fatalf("expected the dashboard to report disabled, got %+v", cfg.Msg)
	}

	// Regenerating (SetupDashboard again) replaces the key outright — the
	// old one must not still work.
	setup2, err := s.SetupDashboard(ctx, connect.NewRequest(&v1.SetupDashboardRequest{FamilyId: fam.Msg.Family.Id}))
	if err != nil {
		t.Fatalf("SetupDashboard (regenerate): %v", err)
	}
	if setup2.Msg.DashboardKey == setup.Msg.DashboardKey {
		t.Fatal("expected regenerating to produce a different key")
	}
}
