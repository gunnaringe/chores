package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strings"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
)

// personalAccessTokenPrefix marks a bearer token as one of ours. It lets
// PersonalTokenOrAuth recognize a candidate without a database lookup for
// every unrelated Authorization header a future integration might send, and
// makes a token accidentally committed somewhere recognizable — including
// by GitHub's own secret scanning — as a Chores credential rather than an
// opaque string.
const personalAccessTokenPrefix = "chorespat_"

func randomPersonalAccessToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return personalAccessTokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// hashPersonalAccessToken is how the token is stored and looked up — never
// the raw value, which exists only in the response that created it and in
// whatever the caller does with it afterwards.
func hashPersonalAccessToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) (string, bool) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	return strings.TrimPrefix(h, prefix), true
}

// PersonalTokenOrAuth is DashboardOrAuth's fallback (see dashboard.go),
// mirroring its shape: a request carrying "Authorization: Bearer
// chorespat_..." is authenticated as the login identity that created the
// token and calls next directly, exactly as if authMgr.RequireAuth had
// authenticated it from a valid session cookie — every existing
// membership/role check downstream (requireRole, boundUserInFamily, ...)
// applies to it unchanged. Anything else (no Authorization header, or a
// Bearer value that doesn't start with our prefix) falls through to
// authMgr.RequireAuth(next), exactly the ordinary cookie-checked path as if
// this wrapper weren't here.
//
// This takes authMgr and calls RequireAuth itself, rather than accepting an
// already-RequireAuth-wrapped next the way a simpler middleware might,
// because RequireAuth unconditionally rejects on the session cookie
// regardless of what identity a request's context already carries — a
// bearer-token request has no cookie at all, so it must never pass through
// RequireAuth on its way to next. Only the no-token fallback path is
// allowed to.
//
// A Bearer value that DOES start with our prefix but doesn't match any
// stored token is rejected outright with a 401 here instead of falling
// through: that would still end up rejected (by the cookie check in turn),
// but with a misleading "login required" instead of naming the actual
// problem — same reasoning as DashboardOrAuth's own invalid-key handling.
func (s *Server) PersonalTokenOrAuth(authMgr *auth.Manager, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok || !strings.HasPrefix(token, personalAccessTokenPrefix) {
			authMgr.RequireAuth(next).ServeHTTP(w, r)
			return
		}
		pat, ok := s.lookupPersonalAccessToken(r.Context(), token)
		if !ok {
			auth.WriteUnauthenticated(w, r, "invalid personal access token")
			return
		}
		if pat.expired {
			auth.WriteUnauthenticated(w, r, "personal access token has expired")
			return
		}
		need, ok := permissionForPath(r.URL.Path)
		if !ok {
			writePermissionDenied(w, r, "personal access tokens can't call this method")
			return
		}
		if !slices.Contains(pat.permissions, need) {
			writePermissionDenied(w, r, "this token lacks the "+permissionLabel(need)+" permission")
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.NewContextWithIdentity(r.Context(), pat.identity)))
	})
}

// tokenTouchInterval is how stale last_used_at may get before a request
// rewrites it. The Home Assistant integration and the Kindle poll with a
// token every few seconds to minutes, and touching it on every call would
// make each of those reads take the single write connection. Settings only
// shows "last used" as a rough "is this still in use", so an hour is plenty.
const tokenTouchInterval = time.Hour

type personalAccessToken struct {
	identity    auth.Identity
	permissions []v1.TokenPermission
	expired     bool
}

// lookupPersonalAccessToken resolves a raw bearer value. ok is false only
// for a token that doesn't exist; an expired one is returned with expired
// set, so the caller can say so instead of "invalid".
func (s *Server) lookupPersonalAccessToken(ctx context.Context, token string) (personalAccessToken, bool) {
	var id, authSubject, name, email string
	var lastUsedAt, permissions, expiresAt sql.NullString
	err := s.ro.QueryRowContext(ctx, `
		SELECT id, auth_subject, identity_name, identity_email, last_used_at, permissions, expires_at
		FROM personal_access_tokens WHERE token_hash = ?
	`, hashPersonalAccessToken(token)).Scan(&id, &authSubject, &name, &email, &lastUsedAt, &permissions, &expiresAt)
	if err != nil {
		return personalAccessToken{}, false
	}
	now := nowUTC()
	pat := personalAccessToken{
		identity:    auth.Identity{Sub: authSubject, Name: name, Email: email},
		permissions: allTokenPermissions(),
		expired:     expiresAt.Valid && expiresAt.String <= formatTime(now),
	}
	if permissions.Valid {
		pat.permissions = decodeTokenPermissions(permissions.String)
	}
	if pat.expired {
		return pat, true
	}
	if !lastUsedAt.Valid || lastUsedAt.String < formatTime(now.Add(-tokenTouchInterval)) {
		// Best-effort: a failed touch shouldn't fail the request it's riding
		// along with, and losing one update doesn't matter for what this is
		// used for.
		if _, err := s.db.ExecContext(ctx, `UPDATE personal_access_tokens SET last_used_at = ? WHERE id = ?`, formatTime(now), id); err != nil {
			log.Printf("touch personal access token %s: %v", id, err)
		}
	}
	return pat, true
}

// identityForPersonalAccessToken is lookupPersonalAccessToken for callers
// that only need to know who a usable token acts as.
func (s *Server) identityForPersonalAccessToken(ctx context.Context, token string) (auth.Identity, bool) {
	pat, ok := s.lookupPersonalAccessToken(ctx, token)
	if !ok || pat.expired {
		return auth.Identity{}, false
	}
	return pat.identity, true
}

// CreatePersonalAccessToken mints a new token for the caller's own login
// identity. Only a login session can reach this: no token may manage
// tokens (see tokenMethodPermission), or a narrow token could mint itself a
// broader one.
func (s *Server) CreatePersonalAccessToken(ctx context.Context, req *connect.Request[v1.CreatePersonalAccessTokenRequest]) (*connect.Response[v1.CreatePersonalAccessTokenResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	name := strings.TrimSpace(req.Msg.GetName())
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	perms, err := validateTokenPermissions(req.Msg.GetPermissions())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var expiresAt sql.NullString
	if req.Msg.ExpiresAt != nil {
		at := req.Msg.GetExpiresAt().AsTime()
		if !at.After(nowUTC()) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("expires_at must be in the future"))
		}
		expiresAt = sql.NullString{String: formatTime(at), Valid: true}
	}
	secret, err := randomPersonalAccessToken()
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO personal_access_tokens (id, auth_subject, name, identity_name, identity_email, token_hash, created_at, permissions, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, identity.Sub, name, identity.Name, identity.Email, hashPersonalAccessToken(secret), formatTime(now), encodeTokenPermissions(perms), expiresAt); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create personal access token: %w", err))
	}

	pbToken := &v1.PersonalAccessToken{Id: id, Name: name, CreatedAt: timestampPB(now), Permissions: perms}
	if expiresAt.Valid {
		pbToken.ExpiresAt = req.Msg.GetExpiresAt()
	}
	return connect.NewResponse(&v1.CreatePersonalAccessTokenResponse{
		Token:  pbToken,
		Secret: secret,
	}), nil
}

// ListPersonalAccessTokens lists every token bound to the caller's own
// login identity — never another identity's, since the query itself is
// scoped by auth_subject rather than trusting a request-supplied filter.
func (s *Server) ListPersonalAccessTokens(ctx context.Context, _ *connect.Request[v1.ListPersonalAccessTokensRequest]) (*connect.Response[v1.ListPersonalAccessTokensResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	rows, err := s.ro.QueryContext(ctx, `
		SELECT id, name, created_at, last_used_at, permissions, expires_at FROM personal_access_tokens
		WHERE auth_subject = ? ORDER BY created_at
	`, identity.Sub)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var tokens []*v1.PersonalAccessToken
	for rows.Next() {
		var id, name, createdAtStr string
		var lastUsedAtStr, permissions, expiresAtStr sql.NullString
		if err := rows.Scan(&id, &name, &createdAtStr, &lastUsedAtStr, &permissions, &expiresAtStr); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		createdAt, err := parseTime(createdAtStr)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		pbToken := &v1.PersonalAccessToken{Id: id, Name: name, CreatedAt: timestampPB(createdAt)}
		if lastUsedAtStr.Valid {
			lastUsedAt, err := parseTime(lastUsedAtStr.String)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			pbToken.LastUsedAt = timestampPB(lastUsedAt)
		}
		if permissions.Valid {
			pbToken.Permissions = decodeTokenPermissions(permissions.String)
		} else {
			pbToken.Permissions = allTokenPermissions()
			pbToken.Legacy = true
		}
		if expiresAtStr.Valid {
			expiresAt, err := parseTime(expiresAtStr.String)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			pbToken.ExpiresAt = timestampPB(expiresAt)
		}
		tokens = append(tokens, pbToken)
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.ListPersonalAccessTokensResponse{Tokens: tokens}), nil
}

// RevokePersonalAccessToken deletes a token immediately — any request
// already using it fails its very next call. Scoped to the caller's own
// login identity the same way ListPersonalAccessTokens is, so a token_id
// belonging to someone else reports NotFound rather than revealing whether
// it exists.
func (s *Server) RevokePersonalAccessToken(ctx context.Context, req *connect.Request[v1.RevokePersonalAccessTokenRequest]) (*connect.Response[v1.RevokePersonalAccessTokenResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	tokenID := req.Msg.GetTokenId()
	if tokenID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("token_id is required"))
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM personal_access_tokens WHERE id = ? AND auth_subject = ?`, tokenID, identity.Sub)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke personal access token: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("token not found"))
	}
	return connect.NewResponse(&v1.RevokePersonalAccessTokenResponse{}), nil
}
