package server

import (
	"context"
	"database/sql"
	"errors"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
)

// Membership and role checks. Every RPC in this package goes through one
// of these before it touches the database.

// currentIdentity returns the caller's login identity. Auth is always
// required now, so RequireAuth guarantees this is present for every real
// request — a false ok this deep means something bypassed that middleware
// (e.g. a Go call straight into an RPC method, as some internal callers
// below do) and every access check treats that as denial, never as "no
// restriction."
func (s *Server) currentIdentity(ctx context.Context) (auth.Identity, bool) {
	return auth.FromContext(ctx)
}

// boundUserInFamily returns the user row the caller's login identity is
// bound to within familyID specifically, or nil if none.
func (s *Server) boundUserInFamily(ctx context.Context, identity auth.Identity, familyID string) (*v1.User, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ? AND family_id = ?`, identity.Sub, familyID).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return s.getUser(ctx, userID)
}

// requireMembership ensures the caller is bound to a user row belonging to
// familyID, regardless of role.
func (s *Server) requireMembership(ctx context.Context, familyID string) error {
	return s.requireRole(ctx, familyID)
}

// requireRole ensures the caller is bound to familyID and, if any roles are
// given, that their role there is one of them. Used to keep
// family-management actions (adding members, managing tasks, inviting
// people, paying out) restricted to parents now that children can have
// their own login and API access.
//
// A request authorized by a dashboard key (see dashboard.go) is handled
// explicitly here rather than falling into the "no identity" branch below:
// it satisfies a plain membership check (len(allowed) == 0) for exactly the
// family the key belongs to, since that's what the Today dashboard's own
// nested calls (ListTaskOccurrences calling ListTasks/ListUsers) need — but
// it never satisfies a role-restricted check. That's deliberate defense in
// depth. The HTTP layer (DashboardOrAuth) already keeps a dashboard-keyed
// request from ever reaching any RPC other than the few the dashboard
// actually uses, but should a future code path ever hand this function a
// dashboard-only context for something else — say, by calling this RPC's
// Go implementation directly, as the nested calls above do — rejecting
// role-restricted checks outright closes that off at the source instead of
// relying solely on the perimeter check.
//
// Below the dashboard case, no identity at all means denial: RequireAuth
// guarantees every real request carries one, so a missing identity here
// means something reached this RPC without going through it.
func (s *Server) requireRole(ctx context.Context, familyID string, allowed ...v1.UserRole) error {
	if dashFamilyID, ok := dashboardFamilyFromContext(ctx); ok {
		if dashFamilyID != familyID {
			return connect.NewError(connect.CodePermissionDenied, errors.New("dashboard access does not extend to this family"))
		}
		if len(allowed) == 0 {
			return nil
		}
		return connect.NewError(connect.CodePermissionDenied, errors.New("dashboard access does not extend to this action"))
	}
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	user, err := s.boundUserInFamily(ctx, identity, familyID)
	if err != nil {
		return err
	}
	if user == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("not a member of this family"))
	}
	if len(allowed) == 0 {
		return nil
	}
	for _, r := range allowed {
		if user.Role == r {
			return nil
		}
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("parents only"))
}

func (s *Server) requireParent(ctx context.Context, familyID string) error {
	return s.requireRole(ctx, familyID, v1.UserRole_USER_ROLE_PARENT)
}

// requireSelfOrParent ensures a bound child can only act on their own
// child_id within familyID — a bound parent there is unrestricted. Callers
// should also call requireMembership/requireParent for familyID first,
// which makes the identity check below unreachable in practice; it stays
// as defense in depth.
func (s *Server) requireSelfOrParent(ctx context.Context, familyID, childID string) error {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	user, err := s.boundUserInFamily(ctx, identity, familyID)
	if err != nil {
		return err
	}
	if user != nil && user.Role == v1.UserRole_USER_ROLE_CHILD && user.Id != childID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("children can only act on their own behalf"))
	}
	return nil
}

// selfFilterForChild returns the child_id filter a list RPC should use: a
// bound child (within familyID) is always forced to see only their own
// data, overriding whatever was requested. A bound parent, or a
// dashboard-authorized request (which has no login identity — the Today
// dashboard is meant to see the whole family, unfiltered, and every caller
// of this function runs requireMembership/dashboard-branch validation
// first, so reaching here with no identity only ever means the latter),
// gets requested back unchanged.
func (s *Server) selfFilterForChild(ctx context.Context, familyID, requested string) (string, error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return requested, nil
	}
	user, err := s.boundUserInFamily(ctx, identity, familyID)
	if err != nil {
		return "", err
	}
	if user != nil && user.Role == v1.UserRole_USER_ROLE_CHILD {
		return user.Id, nil
	}
	return requested, nil
}
