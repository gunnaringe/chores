package server

import (
	"context"
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
	ctx := context.Background()

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
	ctx := context.Background()

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
