package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

func TestGetPushConfig_ReturnsAGeneratedVAPIDKey(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()

	resp, err := s.GetPushConfig(ctx, connect.NewRequest(&v1.GetPushConfigRequest{}))
	if err != nil {
		t.Fatalf("GetPushConfig: %v", err)
	}
	if resp.Msg.VapidPublicKey == "" {
		t.Fatal("expected a non-empty VAPID public key")
	}
}

func TestSubscribeAndUnsubscribeFromPush(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|push-subscribe-tester")

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	parent, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	sub := &v1.PushSubscription{Endpoint: "https://push.example/abc", P256Dh: "p256dh-key", Auth: "auth-key"}
	if _, err := s.SubscribeToPush(ctx, connect.NewRequest(&v1.SubscribeToPushRequest{
		UserId: parent.Msg.User.Id, Subscription: sub,
	})); err != nil {
		t.Fatalf("SubscribeToPush: %v", err)
	}

	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, sub.Endpoint).Scan(&count); err != nil {
		t.Fatalf("query push_subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 subscription row, got %d", count)
	}

	// Re-subscribing with the same endpoint (e.g. re-enabling after
	// disabling) replaces the row rather than duplicating it.
	if _, err := s.SubscribeToPush(ctx, connect.NewRequest(&v1.SubscribeToPushRequest{
		UserId: parent.Msg.User.Id, Subscription: sub,
	})); err != nil {
		t.Fatalf("SubscribeToPush (again): %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, sub.Endpoint).Scan(&count); err != nil {
		t.Fatalf("query push_subscriptions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected re-subscribing to still leave exactly 1 row, got %d", count)
	}

	if _, err := s.UnsubscribeFromPush(ctx, connect.NewRequest(&v1.UnsubscribeFromPushRequest{Endpoint: sub.Endpoint})); err != nil {
		t.Fatalf("UnsubscribeFromPush: %v", err)
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?`, sub.Endpoint).Scan(&count); err != nil {
		t.Fatalf("query push_subscriptions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected the subscription to be removed, got %d rows", count)
	}

	// Unsubscribing an endpoint that was never subscribed is a no-op, not an
	// error.
	if _, err := s.UnsubscribeFromPush(ctx, connect.NewRequest(&v1.UnsubscribeFromPushRequest{Endpoint: "https://push.example/never-existed"})); err != nil {
		t.Fatalf("UnsubscribeFromPush on unknown endpoint: %v", err)
	}
}

func TestSubscribeToPush_RequiresCompleteSubscription(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|push-subscribe-validation-tester")

	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	parent, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	cases := []*v1.PushSubscription{
		{Endpoint: "", P256Dh: "x", Auth: "y"},
		{Endpoint: "https://push.example/x", P256Dh: "", Auth: "y"},
		{Endpoint: "https://push.example/x", P256Dh: "x", Auth: ""},
	}
	for _, sub := range cases {
		if _, err := s.SubscribeToPush(ctx, connect.NewRequest(&v1.SubscribeToPushRequest{
			UserId: parent.Msg.User.Id, Subscription: sub,
		})); codeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("expected InvalidArgument for incomplete subscription %+v, got %v", sub, err)
		}
	}
}

func TestValidatePushEndpoint(t *testing.T) {
	ok := []string{
		"https://fcm.googleapis.com/fcm/send/abc",
		"https://updates.push.services.mozilla.com/wpush/v2/abc",
		"https://web.push.apple.com/abc",
		"https://8.8.8.8/abc",
	}
	bad := []string{
		"http://fcm.googleapis.com/fcm/send/abc", // not https
		"ftp://push.example/x",
		"https:///no-host",
		"https://user:pass@push.example/x",
		"https://127.0.0.1/x",
		"https://10.0.0.5/x",
		"https://192.168.1.1/x",
		"https://169.254.169.254/latest/meta-data",
		"https://100.100.1.1/x", // CGNAT / Tailscale
		"https://[::1]/x",
		"https://[fd00::1]/x",
		"https://[::ffff:10.0.0.1]/x",
		"https://0.0.0.0/x",
		"not a url\x7f",
	}
	for _, e := range ok {
		if err := validatePushEndpoint(e); err != nil {
			t.Errorf("validatePushEndpoint(%q) = %v, want ok", e, err)
		}
	}
	for _, e := range bad {
		if err := validatePushEndpoint(e); err == nil {
			t.Errorf("validatePushEndpoint(%q) = nil, want an error", e)
		}
	}
}

// The subscribe-time check can't see what a hostname resolves to later, so
// the client itself must refuse to connect anywhere non-public. An httptest
// server listens on loopback, which is exactly such an address.
func TestPushClient_RefusesNonPublicAddresses(t *testing.T) {
	hit := false
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer srv.Close()

	client := newPushHTTPClient()
	client.Transport.(*http.Transport).TLSClientConfig = srv.Client().Transport.(*http.Transport).TLSClientConfig
	resp, err := client.Post(srv.URL, "application/octet-stream", nil)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected the push client to refuse a loopback address")
	}
	if hit {
		t.Fatal("request reached the loopback server")
	}
	if !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("expected a non-public address error, got %v", err)
	}
}

func TestSubscribeToPush_RejectsNonHTTPSAndInternalEndpoints(t *testing.T) {
	s := newTestServer(t)
	ctx := withIdentity("auth0|push-ssrf-tester")
	fam, err := s.CreateFamily(ctx, connect.NewRequest(&v1.CreateFamilyRequest{Name: "The Testsons"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	parent, err := s.CreateUser(ctx, connect.NewRequest(&v1.CreateUserRequest{FamilyId: fam.Msg.Family.Id, Name: "Mom", Role: v1.UserRole_USER_ROLE_PARENT}))
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for _, endpoint := range []string{"http://push.example/x", "https://169.254.169.254/x"} {
		_, err := s.SubscribeToPush(ctx, connect.NewRequest(&v1.SubscribeToPushRequest{
			UserId:       parent.Msg.User.Id,
			Subscription: &v1.PushSubscription{Endpoint: endpoint, P256Dh: "k", Auth: "a"},
		}))
		if codeOf(err) != connect.CodeInvalidArgument {
			t.Fatalf("SubscribeToPush(%q): expected InvalidArgument, got %v", endpoint, err)
		}
	}
}

// Knowing another family's endpoint URL must not be enough to take over or
// remove their subscription.
func TestPushSubscription_ScopedToItsOwnerAcrossFamilies(t *testing.T) {
	s := newTestServer(t)
	alice := withIdentity("auth0|push-alice")
	mallory := withIdentity("auth0|push-mallory")

	famA, err := s.CreateFamily(alice, connect.NewRequest(&v1.CreateFamilyRequest{Name: "A"}))
	if err != nil {
		t.Fatalf("CreateFamily A: %v", err)
	}
	famM, err := s.CreateFamily(mallory, connect.NewRequest(&v1.CreateFamilyRequest{Name: "M"}))
	if err != nil {
		t.Fatalf("CreateFamily M: %v", err)
	}
	aliceUser := boundUserID(t, s, "auth0|push-alice", famA.Msg.Family.Id)
	malloryUser := boundUserID(t, s, "auth0|push-mallory", famM.Msg.Family.Id)

	sub := &v1.PushSubscription{Endpoint: "https://push.example/alice-device", P256Dh: "alice-key", Auth: "alice-auth"}
	if _, err := s.SubscribeToPush(alice, connect.NewRequest(&v1.SubscribeToPushRequest{UserId: aliceUser, Subscription: sub})); err != nil {
		t.Fatalf("alice subscribe: %v", err)
	}

	// Mallory re-registers Alice's endpoint with keys of her own choosing.
	_, err = s.SubscribeToPush(mallory, connect.NewRequest(&v1.SubscribeToPushRequest{
		UserId:       malloryUser,
		Subscription: &v1.PushSubscription{Endpoint: sub.Endpoint, P256Dh: "mallory-key", Auth: "mallory-auth"},
	}))
	if codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied taking over another family's endpoint, got %v", err)
	}

	// Mallory's unsubscribe is a silent no-op.
	if _, err := s.UnsubscribeFromPush(mallory, connect.NewRequest(&v1.UnsubscribeFromPushRequest{Endpoint: sub.Endpoint})); err != nil {
		t.Fatalf("mallory unsubscribe: %v", err)
	}
	var owner string
	if err := s.db.QueryRow(`SELECT user_id FROM push_subscriptions WHERE endpoint = ?`, sub.Endpoint).Scan(&owner); err != nil {
		t.Fatalf("subscription should still exist: %v", err)
	}
	if owner != aliceUser {
		t.Fatalf("subscription moved to %s, want %s", owner, aliceUser)
	}

	// Alice refreshing her own subscription's keys is allowed.
	refreshed := &v1.PushSubscription{Endpoint: sub.Endpoint, P256Dh: "alice-key-2", Auth: "alice-auth-2"}
	if _, err := s.SubscribeToPush(alice, connect.NewRequest(&v1.SubscribeToPushRequest{UserId: aliceUser, Subscription: refreshed})); err != nil {
		t.Fatalf("own key refresh: %v", err)
	}
	sub = refreshed

	// The same browser (same keys) signing in to another account still
	// moves the subscription, as it always could.
	if _, err := s.SubscribeToPush(mallory, connect.NewRequest(&v1.SubscribeToPushRequest{UserId: malloryUser, Subscription: sub})); err != nil {
		t.Fatalf("same-keys re-subscribe: %v", err)
	}
}

func boundUserID(t *testing.T, s *Server, sub, familyID string) string {
	t.Helper()
	var id string
	if err := s.db.QueryRow(`SELECT id FROM users WHERE auth_subject = ? AND family_id = ?`, sub, familyID).Scan(&id); err != nil {
		t.Fatalf("bound user for %s: %v", sub, err)
	}
	return id
}
