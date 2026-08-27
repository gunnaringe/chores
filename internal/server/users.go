package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

func (s *Server) CreateUser(ctx context.Context, req *connect.Request[v1.CreateUserRequest]) (*connect.Response[v1.CreateUserResponse], error) {
	familyID := req.Msg.GetFamilyId()
	name := req.Msg.GetName()
	if familyID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and name are required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	roleStr, err := roleToDB(req.Msg.GetRole())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, family_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, familyID, name, roleStr, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create user: %w", err))
	}
	return connect.NewResponse(&v1.CreateUserResponse{
		User: &v1.User{Id: id, FamilyId: familyID, Name: name, Role: req.Msg.GetRole(), CreatedAt: timestampPB(now)},
	}), nil
}

func (s *Server) ListUsers(ctx context.Context, req *connect.Request[v1.ListUsersRequest]) (*connect.Response[v1.ListUsersResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, family_id, name, role, created_at, email, auth_subject FROM users WHERE family_id = ? ORDER BY created_at`,
		familyID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var users []*v1.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		users = append(users, u)
	}
	return connect.NewResponse(&v1.ListUsersResponse{Users: users}), nil
}

// UpdateUser renames a user. It's self-service only — even a parent can't
// rename anyone else this way — so it's gated on a role check first (which,
// unlike a bare requireMembership, also closes it to a dashboard key; see
// requireRole's doc comment) and then an explicit "is this actually you"
// check against the caller's own bound identity.
func (s *Server) UpdateUser(ctx context.Context, req *connect.Request[v1.UpdateUserRequest]) (*connect.Response[v1.UpdateUserResponse], error) {
	userID := req.Msg.GetUserId()
	name := strings.TrimSpace(req.Msg.GetName())
	if userID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id and name are required"))
	}
	target, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.requireRole(ctx, target.FamilyId, v1.UserRole_USER_ROLE_PARENT, v1.UserRole_USER_ROLE_CHILD); err != nil {
		return nil, err
	}
	// requireRole above already guarantees identity is present (it fails
	// closed otherwise), so this check always runs.
	identity, _ := s.currentIdentity(ctx)
	bound, err := s.boundUserInFamily(ctx, identity, target.FamilyId)
	if err != nil {
		return nil, err
	}
	if bound == nil || bound.Id != userID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you can only rename yourself"))
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET name = ? WHERE id = ?`, name, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update user: %w", err))
	}
	updated, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.UpdateUserResponse{User: updated}), nil
}

// countParents returns how many parent rows familyID currently has —
// used to stop the last one from leaving (deleting the family instead is
// how you get rid of the last parent).
func (s *Server) countParents(ctx context.Context, familyID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users WHERE family_id = ? AND role = 'parent'`,
		familyID,
	).Scan(&n)
	return n, err
}

func (s *Server) LeaveFamily(ctx context.Context, req *connect.Request[v1.LeaveFamilyRequest]) (*connect.Response[v1.LeaveFamilyResponse], error) {
	userID := req.Msg.GetUserId()
	if userID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user_id is required"))
	}
	target, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if target.Role != v1.UserRole_USER_ROLE_PARENT {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("only a parent can leave a family this way"))
	}
	if err := s.requireParent(ctx, target.FamilyId); err != nil {
		return nil, err
	}
	// A bound login may only leave as itself, never on another parent's
	// behalf — the same "no impersonating a co-parent" boundary the UI
	// already enforces. requireParent above already guarantees identity is
	// present (it fails closed otherwise), so this check always runs.
	identity, _ := s.currentIdentity(ctx)
	actingUser, err := s.boundUserInFamily(ctx, identity, target.FamilyId)
	if err != nil {
		return nil, err
	}
	if actingUser == nil || actingUser.Id != userID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("you can only leave as yourself"))
	}

	parentCount, err := s.countParents(ctx, target.FamilyId)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if parentCount <= 1 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you're the last parent in this family — delete the family instead of leaving it"))
	}

	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("leave family: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return connect.NewResponse(&v1.LeaveFamilyResponse{}), nil
}

func (s *Server) RemoveChild(ctx context.Context, req *connect.Request[v1.RemoveChildRequest]) (*connect.Response[v1.RemoveChildResponse], error) {
	childID := req.Msg.GetChildId()
	if childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child_id is required"))
	}
	child, err := s.getUser(ctx, childID)
	if err != nil {
		return nil, err
	}
	if child.Role != v1.UserRole_USER_ROLE_CHILD {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("user is not a child"))
	}
	if err := s.requireParent(ctx, child.FamilyId); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, childID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove child: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	return connect.NewResponse(&v1.RemoveChildResponse{}), nil
}
