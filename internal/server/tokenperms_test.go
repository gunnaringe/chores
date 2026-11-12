package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
)

// tokenNeverAllowed are the RPCs deliberately left out of
// tokenMethodPermission. Adding an RPC to the service means putting it in
// one or the other.
var tokenNeverAllowed = []string{
	"CreatePersonalAccessToken", "ListPersonalAccessTokens", "RevokePersonalAccessToken",
	"AcceptInvitation", "SubscribeToPush", "UnsubscribeFromPush",
}

func TestTokenMethodPermission_CoversEveryRPC(t *testing.T) {
	methods := v1.File_chores_v1_chores_proto.Services().ByName("ChoresService").Methods()
	seen := map[string]bool{}
	for i := 0; i < methods.Len(); i++ {
		name := string(methods.Get(i).Name())
		seen[name] = true
		_, mapped := tokenMethodPermission[name]
		never := slices.Contains(tokenNeverAllowed, name)
		if mapped == never {
			t.Errorf("%s must be in exactly one of tokenMethodPermission and tokenNeverAllowed", name)
		}
	}
	for name := range tokenMethodPermission {
		if !seen[name] {
			t.Errorf("tokenMethodPermission has %s, which isn't an RPC", name)
		}
	}
}

func TestTokenPermissions_RoundTrip(t *testing.T) {
	all := allTokenPermissions()
	if got := decodeTokenPermissions(encodeTokenPermissions(all)); !slices.Equal(got, all) {
		t.Fatalf("round trip lost permissions: %v", got)
	}
	if got := decodeTokenPermissions("read admin-of-everything tick"); len(got) != 2 {
		t.Fatalf("unknown names must be dropped, got %v", got)
	}
}

// callWithToken sends one request for method through the same chain
// cmd/chores/main.go uses, with a handler that just answers 200.
func callWithToken(t *testing.T, s *Server, secret, method string) *httptest.ResponseRecorder {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, choresServicePrefix+method, strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	s.DashboardOrAuth(testAuthManager(t), ok).ServeHTTP(rec, req)
	return rec
}

func createToken(t *testing.T, s *Server, ctx context.Context, req *v1.CreatePersonalAccessTokenRequest) string {
	t.Helper()
	created, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(req))
	if err != nil {
		t.Fatalf("CreatePersonalAccessToken: %v", err)
	}
	return created.Msg.Secret
}

func TestTokenPermissions_DefaultsReadAndTickOnly(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-defaults")
	secret := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{Name: "wall"})

	for method, want := range map[string]int{
		"ListTaskOccurrences":       http.StatusOK,
		"CompleteTask":              http.StatusOK,
		"CreateTask":                http.StatusForbidden,
		"CreatePayout":              http.StatusForbidden,
		"DeleteFamily":              http.StatusForbidden,
		"CreatePersonalAccessToken": http.StatusForbidden,
		"NoSuchMethod":              http.StatusForbidden,
	} {
		if got := callWithToken(t, s, secret, method).Code; got != want {
			t.Errorf("%s: got %d, want %d", method, got, want)
		}
	}
	rec := callWithToken(t, s, secret, "CreateTask")
	if !strings.Contains(rec.Body.String(), "Manage tasks") {
		t.Errorf("refusal should name the missing permission, got %s", rec.Body.String())
	}
}

func TestTokenPermissions_GrantedPermissionAllows(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-chores")
	secret := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{
		Name:        "ha",
		Permissions: []v1.TokenPermission{v1.TokenPermission_TOKEN_PERMISSION_MANAGE_CHORES},
	})
	if got := callWithToken(t, s, secret, "CreateTask").Code; got != http.StatusOK {
		t.Errorf("CreateTask: got %d, want 200", got)
	}
	// Exactly what was asked for: not READ on top.
	if got := callWithToken(t, s, secret, "ListTasks").Code; got != http.StatusForbidden {
		t.Errorf("ListTasks: got %d, want 403", got)
	}
}

// A token from before permissions existed keeps working for everything a
// token may do — but still can't manage tokens.
func TestTokenPermissions_LegacyTokenKeepsAccess(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-legacy")
	secret := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{Name: "old"})
	if _, err := s.db.Exec(`UPDATE personal_access_tokens SET permissions = NULL`); err != nil {
		t.Fatal(err)
	}
	if got := callWithToken(t, s, secret, "DeleteFamily").Code; got != http.StatusOK {
		t.Errorf("DeleteFamily: got %d, want 200", got)
	}
	if got := callWithToken(t, s, secret, "CreatePersonalAccessToken").Code; got != http.StatusForbidden {
		t.Errorf("CreatePersonalAccessToken: got %d, want 403", got)
	}
	list, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if tok := list.Msg.Tokens[0]; !tok.Legacy || len(tok.Permissions) != len(allTokenPermissions()) {
		t.Errorf("legacy token should list as legacy with every permission, got %+v", tok)
	}
}

func TestTokenPermissions_Expiry(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-expiry")

	if _, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{
		Name: "past", ExpiresAt: timestamppb.New(nowUTC().Add(-time.Hour)),
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("an expiry in the past should be InvalidArgument, got %v", err)
	}

	expiresAt := nowUTC().Add(24 * time.Hour).Truncate(time.Second)
	secret := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{Name: "soon", ExpiresAt: timestamppb.New(expiresAt)})
	if got := callWithToken(t, s, secret, "ListTasks").Code; got != http.StatusOK {
		t.Fatalf("before expiry: got %d, want 200", got)
	}
	list, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if got := list.Msg.Tokens[0].GetExpiresAt().AsTime(); !got.Equal(expiresAt) {
		t.Fatalf("listed expiry %v, want %v", got, expiresAt)
	}

	if _, err := s.db.Exec(`UPDATE personal_access_tokens SET expires_at = ?`, formatTime(nowUTC().Add(-time.Minute))); err != nil {
		t.Fatal(err)
	}
	rec := callWithToken(t, s, secret, "ListTasks")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("after expiry: got %d %s, want 401 naming expiry", rec.Code, rec.Body.String())
	}
}

func TestTokenPermissions_RejectsUnknownPermission(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-unknown")
	_, err := s.CreatePersonalAccessToken(ctx, connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{
		Name: "bad", Permissions: []v1.TokenPermission{v1.TokenPermission_TOKEN_PERMISSION_UNSPECIFIED},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}

// callWithTokenBody is callWithToken with the real service behind it, so
// the family checks inside the RPCs run too.
func callWithTokenBody(t *testing.T, s *Server, secret, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	_, handler := choresv1connect.NewChoresServiceHandler(s, JSONCodecOption())
	req := httptest.NewRequest(http.MethodPost, choresServicePrefix+method, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	s.DashboardOrAuth(testAuthManager(t), handler).ServeHTTP(rec, req)
	return rec
}

func TestTokenFamilies_LimitToSomeFamilies(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|perm-families")
	famA, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "A", ParentName: "P"}))
	if err != nil {
		t.Fatal(err)
	}
	famB, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "B", ParentName: "P"}))
	if err != nil {
		t.Fatal(err)
	}
	a, b := famA.Msg.Family.Id, famB.Msg.Family.Id

	limited := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{
		Name:        "only A",
		Permissions: allTokenPermissions(),
		FamilyIds:   []string{a},
	})
	if got := callWithTokenBody(t, s, limited, "ListTasks", `{"familyId":"`+a+`"}`).Code; got != http.StatusOK {
		t.Errorf("ListTasks in A: got %d, want 200", got)
	}
	rec := callWithTokenBody(t, s, limited, "ListTasks", `{"familyId":"`+b+`"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "limited to other families") {
		t.Errorf("ListTasks in B: got %d %s, want 403", rec.Code, rec.Body.String())
	}
	if got := callWithTokenBody(t, s, limited, "CreateFamily", `{"name":"C"}`).Code; got != http.StatusForbidden {
		t.Errorf("CreateFamily: got %d, want 403", got)
	}
	rec = callWithTokenBody(t, s, limited, "ListFamilies", `{}`)
	if !strings.Contains(rec.Body.String(), a) || strings.Contains(rec.Body.String(), b) {
		t.Errorf("ListFamilies should list only A, got %s", rec.Body.String())
	}
	rec = callWithTokenBody(t, s, limited, "GetMyMembership", `{}`)
	if !strings.Contains(rec.Body.String(), a) || strings.Contains(rec.Body.String(), b) {
		t.Errorf("GetMyMembership should list only A, got %s", rec.Body.String())
	}

	// The default: every family.
	all := createToken(t, s, ctx, &v1.CreatePersonalAccessTokenRequest{Name: "all"})
	if got := callWithTokenBody(t, s, all, "ListTasks", `{"familyId":"`+b+`"}`).Code; got != http.StatusOK {
		t.Errorf("unlimited token, ListTasks in B: got %d, want 200", got)
	}

	list, err := s.ListPersonalAccessTokens(ctx, connect.NewRequest(&v1.ListPersonalAccessTokensRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range list.Msg.Tokens {
		want := map[string]int{"only A": 1, "all": 0}[tok.Name]
		if len(tok.FamilyIds) != want {
			t.Errorf("%s lists families %v", tok.Name, tok.FamilyIds)
		}
	}
}

func TestTokenFamilies_OnlyYourOwnFamilies(t *testing.T) {
	s := newTestServer(t)
	other, err := s.CreateFamily(withIdentity("auth0|someone-else"), connect.NewRequest(&v1.CreateFamilyRequest{Name: "Theirs", ParentName: "P"}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreatePersonalAccessToken(withIdentity("auth0|perm-families-own"), connect.NewRequest(&v1.CreatePersonalAccessTokenRequest{
		Name: "x", FamilyIds: []string{other.Msg.Family.Id},
	}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("got %v, want InvalidArgument", err)
	}
}
