package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	webpush "github.com/SherClockHolmes/webpush-go"
	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

// pushSubscriber is the VAPID JWT "sub" claim. Push services don't verify
// it's deliverable — it's just a contact-of-last-resort the spec requires.
const pushSubscriber = "mailto:chores-app@localhost"

func (s *Server) getAppSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Server) setAppSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO app_settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// ensureVAPIDKeys loads the VAPID keypair from app_settings, generating and
// persisting one on first run. The keypair is stable for the life of the
// database: rotating it would invalidate every existing browser
// subscription, which would otherwise need re-subscribing anyway.
func (s *Server) ensureVAPIDKeys() error {
	pub, err := s.getAppSetting("vapid_public_key")
	if err != nil {
		return fmt.Errorf("load vapid_public_key: %w", err)
	}
	priv, err := s.getAppSetting("vapid_private_key")
	if err != nil {
		return fmt.Errorf("load vapid_private_key: %w", err)
	}
	if pub != "" && priv != "" {
		s.vapidPublicKey = pub
		s.vapidPrivateKey = priv
		return nil
	}

	newPriv, newPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return fmt.Errorf("generate VAPID keys: %w", err)
	}
	if err := s.setAppSetting("vapid_public_key", newPub); err != nil {
		return fmt.Errorf("save vapid_public_key: %w", err)
	}
	if err := s.setAppSetting("vapid_private_key", newPriv); err != nil {
		return fmt.Errorf("save vapid_private_key: %w", err)
	}
	s.vapidPublicKey = newPub
	s.vapidPrivateKey = newPriv
	return nil
}

func (s *Server) GetPushConfig(ctx context.Context, _ *connect.Request[v1.GetPushConfigRequest]) (*connect.Response[v1.GetPushConfigResponse], error) {
	return connect.NewResponse(&v1.GetPushConfigResponse{VapidPublicKey: s.vapidPublicKey}), nil
}

func (s *Server) SubscribeToPush(ctx context.Context, req *connect.Request[v1.SubscribeToPushRequest]) (*connect.Response[v1.SubscribeToPushResponse], error) {
	userID := req.Msg.GetUserId()
	sub := req.Msg.GetSubscription()
	if userID == "" || sub.GetEndpoint() == "" || sub.GetP256Dh() == "" || sub.GetAuth() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id and a complete subscription are required"))
	}
	user, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.requireSelfOrParent(ctx, user.FamilyId, userID); err != nil {
		return nil, err
	}

	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO push_subscriptions (id, user_id, family_id, endpoint, p256dh, auth, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (endpoint) DO UPDATE SET user_id = excluded.user_id, family_id = excluded.family_id, p256dh = excluded.p256dh, auth = excluded.auth`,
		newID(), userID, user.FamilyId, sub.GetEndpoint(), sub.GetP256Dh(), sub.GetAuth(), formatTime(nowUTC()),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("save push subscription: %w", err))
	}
	return connect.NewResponse(&v1.SubscribeToPushResponse{}), nil
}

func (s *Server) UnsubscribeFromPush(ctx context.Context, req *connect.Request[v1.UnsubscribeFromPushRequest]) (*connect.Response[v1.UnsubscribeFromPushResponse], error) {
	endpoint := req.Msg.GetEndpoint()
	if endpoint == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("endpoint is required"))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE endpoint = ?`, endpoint); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove push subscription: %w", err))
	}
	return connect.NewResponse(&v1.UnsubscribeFromPushResponse{}), nil
}

// actingUserID returns the user row the caller's login identity is bound to
// within familyID, or "" if there is none (local-testing mode, or a login
// not bound to this family) — used to exclude the person who just completed
// a task from their own "task completed" notification.
func (s *Server) actingUserID(ctx context.Context, familyID string) string {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return ""
	}
	user, err := s.boundUserInFamily(ctx, identity, familyID)
	if err != nil || user == nil {
		return ""
	}
	return user.Id
}

// notifyTaskCompleted pushes a "task completed" notification to every other
// device in the family that has subscribed, best-effort: a delivery failure
// here never affects the CompleteTask response, since by the time this runs
// the completion is already recorded. Intended to be called via `go`, so it
// takes plain values rather than a request-scoped context (which would be
// canceled the moment the handler returns).
func (s *Server) notifyTaskCompleted(familyID, excludeUserID, childName, taskTitle string, amountCents int64) {
	if s.vapidPublicKey == "" || s.vapidPrivateKey == "" {
		return
	}

	rows, err := s.db.Query(
		`SELECT id, endpoint, p256dh, auth FROM push_subscriptions WHERE family_id = ? AND user_id != ?`,
		familyID, excludeUserID,
	)
	if err != nil {
		log.Printf("push: query subscriptions: %v", err)
		return
	}
	type target struct{ id, endpoint, p256dh, auth string }
	var targets []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.id, &t.endpoint, &t.p256dh, &t.auth); err != nil {
			log.Printf("push: scan subscription: %v", err)
			continue
		}
		targets = append(targets, t)
	}
	rows.Close()
	if len(targets) == 0 {
		return
	}

	payload, err := json.Marshal(map[string]string{
		"title": "Chores",
		"body":  fmt.Sprintf("%s completed \"%s\" (kr %.2f)", childName, taskTitle, float64(amountCents)/100),
	})
	if err != nil {
		log.Printf("push: build payload: %v", err)
		return
	}

	for _, t := range targets {
		resp, err := webpush.SendNotification(payload, &webpush.Subscription{
			Endpoint: t.endpoint,
			Keys:     webpush.Keys{P256dh: t.p256dh, Auth: t.auth},
		}, &webpush.Options{
			Subscriber:      pushSubscriber,
			VAPIDPublicKey:  s.vapidPublicKey,
			VAPIDPrivateKey: s.vapidPrivateKey,
			TTL:             30,
		})
		if err != nil {
			log.Printf("push: send to subscription %s: %v", t.id, err)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			// The push service says this subscription no longer exists
			// (browser unsubscribed, or it expired) — stop trying it.
			if _, err := s.db.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, t.id); err != nil {
				log.Printf("push: remove stale subscription %s: %v", t.id, err)
			}
		}
	}
}
