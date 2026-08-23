package server

import (
	"context"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/ukelonn/v1"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/db"
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
	if membership.Msg.User.Name != "Mom" {
		t.Fatalf("expected bound user name %q, got %q", "Mom", membership.Msg.User.Name)
	}
	if membership.Msg.Family.Id != resp.Msg.Family.Id {
		t.Fatalf("bound family %q does not match created family %q", membership.Msg.Family.Id, resp.Msg.Family.Id)
	}
	if !membership.Msg.User.AuthBound {
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
		FamilyId: famB.Msg.Family.Id, Title: "Sneaky task", Schedule: "0 0 * * *", PriceCents: 100,
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
	task, err := s.CreateTask(ctxA, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famA.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", PriceCents: 100,
	}))
	if err != nil {
		t.Fatalf("CreateTask A: %v", err)
	}
	child, err := s.CreateUser(ctxA, connect.NewRequest(&v1.CreateUserRequest{
		FamilyId: famA.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD,
	}))
	if err != nil {
		t.Fatalf("CreateUser A: %v", err)
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
				FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", PriceCents: 100,
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
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", PriceCents: 100,
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
		FamilyId: fam.Msg.Family.Id, Title: "Dishes", Schedule: "0 0 * * *", PriceCents: 100,
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
