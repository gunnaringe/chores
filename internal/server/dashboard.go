package server

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
)

// DashboardKeyHeader carries the kiosk dashboard's bearer credential on the
// handful of RPCs it's allowed to call — see DashboardOrAuth.
const DashboardKeyHeader = "X-Dashboard-Key"

// dashboardAllowedMethods is the complete list of RPCs a dashboard key can
// authorize, keyed by the exact Connect request path. This is deliberately
// an allowlist enforced at the HTTP layer, before any request reaches the
// RPC handlers: a dashboard-authorized request has no login identity, and
// requireRole's dashboard branch (see its doc comment) only ever satisfies
// a plain membership check there, never a role-restricted one. Keeping the
// bypass itself scoped to named paths — rather than trying to teach every
// identity check in server.go a third state — is what keeps a leaked
// dashboard key from being usable for anything beyond what the Today
// dashboard actually does.
var dashboardAllowedMethods = map[string]bool{
	"/chores.v1.ChoresService/ListChildSummaries":  true,
	"/chores.v1.ChoresService/ListTaskOccurrences": true,
	"/chores.v1.ChoresService/CompleteTask":        true,
	"/chores.v1.ChoresService/UncompleteTask":      true,
}

type dashboardCtxKey struct{}

func newContextWithDashboardFamily(ctx context.Context, familyID string) context.Context {
	return context.WithValue(ctx, dashboardCtxKey{}, familyID)
}

func dashboardFamilyFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(dashboardCtxKey{}).(string)
	return id, ok
}

// authorizedForDashboard reports whether ctx carries dashboard authorization
// for exactly familyID — used by the RPCs in dashboardAllowedMethods to skip
// their normal identity check.
func (s *Server) authorizedForDashboard(ctx context.Context, familyID string) bool {
	dashFamilyID, ok := dashboardFamilyFromContext(ctx)
	return ok && familyID != "" && dashFamilyID == familyID
}

// resolvedFamilyID prefers the family a dashboard key already resolved to
// over whatever the client sent (which the kiosk frontend leaves empty,
// since a bearer key rather than a chosen family is what identifies it) —
// this also means a dashboard key can never be paired with a spoofed
// family_id to reach across families.
func (s *Server) resolvedFamilyID(ctx context.Context, requested string) string {
	if famID, ok := dashboardFamilyFromContext(ctx); ok {
		return famID
	}
	return requested
}

func (s *Server) familyIDForDashboardKey(key string) (string, bool) {
	if key == "" {
		return "", false
	}
	var familyID string
	if err := s.db.QueryRow(`SELECT id FROM families WHERE dashboard_key = ?`, key).Scan(&familyID); err != nil {
		return "", false
	}
	return familyID, true
}

// DashboardOrAuth wraps the Connect handler. A request for one of
// dashboardAllowedMethods, carrying a valid dashboard key, bypasses the
// normal login gate entirely and is tagged with family-scoped dashboard
// context instead of a login identity. A request for one of those same
// methods carrying an invalid key is rejected outright with a 401, rather
// than falling through to authMgr.RequireAuth: that fallback would still
// correctly reject the request (login is always required now), but with
// a generic "login required" body instead of "invalid dashboard key" —
// the wrong message for a kiosk that has no login flow to send someone
// through. Only when the header is absent entirely does a request fall
// straight through to authMgr.RequireAuth, exactly as if this wrapper
// weren't here — that's the ordinary logged-in path, since these same
// RPCs also back the normal Today tab.
func (s *Server) DashboardOrAuth(authMgr *auth.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if dashboardAllowedMethods[r.URL.Path] {
			if key := r.Header.Get(DashboardKeyHeader); key != "" {
				familyID, ok := s.familyIDForDashboardKey(key)
				if !ok {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"invalid dashboard key"}`))
					return
				}
				next.ServeHTTP(w, r.WithContext(newContextWithDashboardFamily(r.Context(), familyID)))
				return
			}
		}
		authMgr.RequireAuth(next).ServeHTTP(w, r)
	})
}

func randomDashboardKey() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Server) GetDashboardConfig(ctx context.Context, req *connect.Request[v1.GetDashboardConfigRequest]) (*connect.Response[v1.GetDashboardConfigResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	var key sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT dashboard_key FROM families WHERE id = ?`, familyID).Scan(&key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetDashboardConfigResponse{Enabled: key.Valid && key.String != "", DashboardKey: key.String}), nil
}

func (s *Server) SetupDashboard(ctx context.Context, req *connect.Request[v1.SetupDashboardRequest]) (*connect.Response[v1.SetupDashboardResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	key, err := randomDashboardKey()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE families SET dashboard_key = ? WHERE id = ?`, key, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("setup dashboard: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
	}
	return connect.NewResponse(&v1.SetupDashboardResponse{DashboardKey: key}), nil
}

func (s *Server) DisableDashboard(ctx context.Context, req *connect.Request[v1.DisableDashboardRequest]) (*connect.Response[v1.DisableDashboardResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE families SET dashboard_key = NULL WHERE id = ?`, familyID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("disable dashboard: %w", err))
	}
	return connect.NewResponse(&v1.DisableDashboardResponse{}), nil
}
