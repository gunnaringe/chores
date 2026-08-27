package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
)

func testAuthManager(t *testing.T) *auth.Manager {
	t.Helper()
	m, err := auth.NewManager(auth.Config{Domain: "tenant.eu.auth0.com", ClientID: "client-id", ClientSecret: "client-secret"})
	if err != nil {
		t.Fatalf("auth.NewManager: %v", err)
	}
	return m
}

func TestPersonalAccessToken_CreateListRevoke(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|pat-owner")

	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "laptop cron job"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}
	if created.Msg.Secret == "" {
		t.Fatal("expected a non-empty secret")
	}
	if !strings.HasPrefix(created.Msg.Secret, personalAccessTokenPrefix) {
		t.Fatalf("expected secret to start with %q, got %q", personalAccessTokenPrefix, created.Msg.Secret)
	}
	if created.Msg.Token.Name != "laptop cron job" {
		t.Fatalf("expected name %q, got %q", "laptop cron job", created.Msg.Token.Name)
	}
	if created.Msg.Token.LastUsedAt != nil {
		t.Fatal("expected a freshly created token to report no last-used time")
	}

	list, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens: %v", err)
	}
	if len(list.Msg.Tokens) != 1 || list.Msg.Tokens[0].Id != created.Msg.Token.Id {
		t.Fatalf("expected the one created token listed back, got %+v", list.Msg.Tokens)
	}

	if _, err := s.RevokePersonalAccessToken(ctx, connect.NewRequest(&v1.RevokePersonalAccessTokenRequest{TokenId: created.Msg.Token.Id})); err != nil {
		t.Fatalf("RevokePersonalAccessToken: %v", err)
	}

	listAfter, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens (after revoke): %v", err)
	}
	if len(listAfter.Msg.Tokens) != 0 {
		t.Fatalf("expected no tokens after revoking the only one, got %+v", listAfter.Msg.Tokens)
	}

	// The revoked token no longer authenticates anything.
	if _, ok := s.identityForPersonalAccessToken(context.Background(), created.Msg.Secret); ok {
		t.Fatal("expected a revoked token to no longer resolve to an identity")
	}
}

func TestPersonalAccessToken_RequiresLogin(t *testing.T) {
	s := newTestServer(t)
	anon := context.Background()

	if _, err := s.CreatePersonalAccessToken(anon, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "x"})); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected Unauthenticated creating a token with no identity, got %v", err)
	}
	if _, err := s.ListPersonalAccessTokens(anon, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{})); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected Unauthenticated listing tokens with no identity, got %v", err)
	}
	if _, err := s.RevokePersonalAccessToken(anon, connect.NewRequest(&v1.RevokePersonalAccessTokenRequest{TokenId: "whatever"})); codeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("expected Unauthenticated revoking with no identity, got %v", err)
	}
}

func TestPersonalAccessToken_NameRequired(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|pat-blank-name")
	if _, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "   "})); codeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for a blank name, got %v", err)
	}
}

// TestPersonalAccessToken_ScopedToOwnIdentity is the core security property:
// one login can never see or revoke another login's tokens, even by a
// correctly-guessed id.
func TestPersonalAccessToken_ScopedToOwnIdentity(t *testing.T) {
	s := newTestServer(t)
	ctxA := withIdentity("auth0|pat-owner-a")
	ctxB := withIdentity("auth0|pat-owner-b")

	created, err := s.CreatePersonalAccessToken(ctxA, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "A's token"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}

	listB, err := s.ListPersonalAccessTokens(ctxB, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens as B: %v", err)
	}
	if len(listB.Msg.Tokens) != 0 {
		t.Fatalf("expected B to see none of A's tokens, got %+v", listB.Msg.Tokens)
	}

	if _, err := s.RevokePersonalAccessToken(ctxB, connect.NewRequest(&v1.RevokePersonalAccessTokenRequest{TokenId: created.Msg.Token.Id})); codeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected NotFound when B revokes A's token id, got %v", err)
	}

	// Still there — B's attempt didn't quietly delete it.
	listA, err := s.ListPersonalAccessTokens(ctxA, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens as A: %v", err)
	}
	if len(listA.Msg.Tokens) != 1 {
		t.Fatalf("expected A's token to survive B's failed revoke attempt, got %+v", listA.Msg.Tokens)
	}
}

// TestPersonalAccessToken_AuthenticatesAsTheCreatingIdentity is the whole
// point of the feature: a token authenticates exactly as the login that
// created it would, in every family that login belongs to — not just the
// one open in the browser tab when the token was minted.
func TestPersonalAccessToken_AuthenticatesAsTheCreatingIdentity(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|pat-multi-family")

	famA, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family A", ParentName: "Parent"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famB, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Family B", ParentName: "Parent"}))
	if err != nil {
		t.Fatalf("CreateFamily B: %v", err)
	}
	child, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: famA.Msg.Family.Id, Name: "Kid", Role: v1.UserRole_USER_ROLE_CHILD}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "script"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}

	identity, ok := s.identityForPersonalAccessToken(context.Background(), created.Msg.Secret)
	if !ok {
		t.Fatal("expected the freshly created token to resolve to an identity")
	}
	if identity.Sub != "auth0|pat-multi-family" {
		t.Fatalf("expected resolved identity sub to match the creating login, got %q", identity.Sub)
	}
	tokenCtx := auth.NewContextWithIdentity(context.Background(), identity)

	// Same access the original session has, in both families it belongs to.
	if _, err := s.ListUsers(tokenCtx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: famA.Msg.Family.Id})); err != nil {
		t.Fatalf("ListUsers (family A) via token: %v", err)
	}
	if _, err := s.ListUsers(tokenCtx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: famB.Msg.Family.Id})); err != nil {
		t.Fatalf("ListUsers (family B) via token: %v", err)
	}
	if _, err := s.CreateTask(tokenCtx, connect.NewRequest(&v1.CreateTaskRequest{
		FamilyId: famA.Msg.Family.Id, Title: "Via token", Schedule: cronSchedule("0 0 * * *"), Price: money(50),
		ChildIds: []string{child.Msg.User.Id},
	})); err != nil {
		t.Fatalf("CreateTask via token: %v", err)
	}

	// last_used_at is touched by resolving the token above.
	list, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatalf("ListPersonalAccessTokens: %v", err)
	}
	if len(list.Msg.Tokens) != 1 || list.Msg.Tokens[0].LastUsedAt == nil {
		t.Fatalf("expected last_used_at to be set after the token was used, got %+v", list.Msg.Tokens)
	}
}

// TestPersonalAccessToken_OrphanedAfterLeavingEveryFamily_GrantsNothing
// covers the design decision documented on the personal_access_tokens
// schema: a token isn't deleted when the login it belongs to is removed
// from every family, it just stops being useful — the same as a lingering
// session cookie for that identity would.
func TestPersonalAccessToken_OrphanedAfterLeavingEveryFamily_GrantsNothing(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|pat-orphan")

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Temp Family", ParentName: "Parent"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "orphan"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}
	if _, err := s.DeleteFamily(ctx, connect.NewRequest(&v1.DeleteFamilyRequest{FamilyId: fam.Msg.Family.Id})); err != nil {
		t.Fatalf("DeleteFamily: %v", err)
	}

	identity, ok := s.identityForPersonalAccessToken(context.Background(), created.Msg.Secret)
	if !ok {
		t.Fatal("expected the token to still resolve to an identity even once unbound from every family")
	}
	tokenCtx := auth.NewContextWithIdentity(context.Background(), identity)
	if _, err := s.ListUsers(tokenCtx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id})); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied for an orphaned token's identity, got %v", err)
	}
}

// TestPersonalTokenOrAuth_FallsThroughToCookieCheck asserts that anything
// not recognizable as one of our tokens (no header, or a Bearer value with
// the wrong prefix) is treated as "not ours" and reaches the real fallback
// — the ordinary session-cookie check — rather than being consumed or
// misclassified by the token-checking branch. With no session cookie
// present either, that fallback rejects the request itself, so next is
// never called; the assertion is on *which* check rejected it (the cookie
// check's "login required", not PersonalTokenOrAuth's own "invalid
// personal access token").
func TestPersonalTokenOrAuth_FallsThroughToCookieCheck(t *testing.T) {
	s := newTestServer(t)
	authMgr := testAuthManager(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("expected the cookie check to reject the request before reaching next")
	})
	handler := s.PersonalTokenOrAuth(authMgr, next)

	for _, authHeader := range []string{"", "Bearer some-other-kind-of-token", "Basic dXNlcjpwYXNz"} {
		req := httptest.NewRequest(http.MethodPost, "/chores.v1.ChoresService/ListUsers", nil)
		if authHeader != "" {
			req.Header.Set("Authorization", authHeader)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("Authorization=%q: expected 401 from the cookie check, got %d", authHeader, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "login required") {
			t.Fatalf("Authorization=%q: expected the ordinary cookie-check rejection, got %q", authHeader, rec.Body.String())
		}
	}
}

func TestPersonalTokenOrAuth_InvalidTokenRejectedOutright(t *testing.T) {
	s := newTestServer(t)
	authMgr := testAuthManager(t)
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})
	handler := s.PersonalTokenOrAuth(authMgr, next)

	req := httptest.NewRequest(http.MethodPost, "/chores.v1.ChoresService/ListUsers", nil)
	req.Header.Set("Authorization", "Bearer "+personalAccessTokenPrefix+"not-a-real-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("expected an invalid personal access token to be rejected before reaching next")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for an invalid personal access token, got %d", rec.Code)
	}
}

// TestPersonalTokenOrAuth_EndToEnd exercises the full HTTP-layer path a real
// API caller would take, in contrast to the tests above which resolve a
// token directly against the database.
func TestPersonalTokenOrAuth_EndToEnd(t *testing.T) {
	s := newTestServer(t)
	authMgr := testAuthManager(t)
	ctx := withIdentity("auth0|pat-http-tester")
	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "http test"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}

	var gotSub string
	var familyIDInHandler string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.FromContext(r.Context())
		if !ok {
			t.Fatal("expected an identity to be attached by PersonalTokenOrAuth")
		}
		gotSub = identity.Sub
		if err := s.requireMembership(r.Context(), familyIDInHandler); err != nil {
			t.Fatalf("requireMembership inside the handler: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	familyIDInHandler = fam.Msg.Family.Id

	req := httptest.NewRequest(http.MethodPost, "/chores.v1.ChoresService/ListUsers", nil)
	req.Header.Set("Authorization", "Bearer "+created.Msg.Secret)
	rec := httptest.NewRecorder()
	s.PersonalTokenOrAuth(authMgr, next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotSub != "auth0|pat-http-tester" {
		t.Fatalf("expected the attached identity's sub to match the creating login, got %q", gotSub)
	}
}

// TestDashboardOrAuth_AcceptsPersonalAccessToken exercises the exact
// composition cmd/chores/main.go wires up — DashboardOrAuth wrapping the
// real handler, with PersonalTokenOrAuth as its fallback — rather than
// calling PersonalTokenOrAuth directly the way the tests above do.
//
// This is a deliberate regression test: PersonalTokenOrAuth passing its own
// unit tests once wasn't enough to prove the feature worked, because
// DashboardOrAuth's fallback used to call authMgr.RequireAuth(next)
// directly. authMgr.RequireAuth ignores whatever identity a request's
// context already carries and rejects purely on the session cookie — so a
// bearer-token request with no cookie 401'd with "login required" even
// though PersonalTokenOrAuth had already (correctly, per its own tests)
// resolved and attached an identity one layer up. Composing the two the
// way main.go actually does is the only way to catch that.
func TestDashboardOrAuth_AcceptsPersonalAccessToken(t *testing.T) {
	s := newTestServer(t)
	authMgr := testAuthManager(t)
	ctx := withIdentity("auth0|pat-full-chain")

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons", ParentName: "Parent"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{Name: "full chain test"}))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.ListUsers(r.Context(), connect.NewRequest(&v1.ListUsersRequest{FamilyId: fam.Msg.Family.Id})); err != nil {
			t.Fatalf("ListUsers inside the handler: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	// A bearer token and deliberately NO session cookie — this is exactly
	// what an external script calling the API would send.
	req := httptest.NewRequest(http.MethodPost, "/chores.v1.ChoresService/ListUsers", nil)
	req.Header.Set("Authorization", "Bearer "+created.Msg.Secret)
	rec := httptest.NewRecorder()
	s.DashboardOrAuth(authMgr, next).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 via DashboardOrAuth with a personal access token and no cookie, got %d: %s", rec.Code, rec.Body.String())
	}
}
