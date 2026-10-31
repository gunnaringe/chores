package server

import (
	"testing"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

// The membership cache lives for one request: within it, repeated checks
// don't go back to the database; a new request sees changes immediately.
func TestMembershipCache_ScopedToOneRequest(t *testing.T) {
	s := newTestServer(t)
	base := withIdentity("auth0|cache-tester")
	fam, err := s.CreateFamily(base, connect.NewRequest(&v1.CreateFamilyRequest{Name: "Cached"}))
	if err != nil {
		t.Fatalf("CreateFamily: %v", err)
	}
	familyID := fam.Msg.Family.Id

	req := withMembershipCache(base)
	if err := s.requireParent(req, familyID); err != nil {
		t.Fatalf("requireParent: %v", err)
	}

	if _, err := s.db.Exec(`UPDATE users SET auth_subject = NULL WHERE auth_subject = ?`, "auth0|cache-tester"); err != nil {
		t.Fatal(err)
	}

	if err := s.requireParent(req, familyID); err != nil {
		t.Fatalf("same request should reuse its cached answer, got %v", err)
	}
	if err := s.requireParent(withMembershipCache(base), familyID); codeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("a new request must see the removal, got %v", err)
	}
}
