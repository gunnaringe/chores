// Package server implements the ChoresService Connect API on top of SQLite.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/scheduling"
)

type Server struct {
	choresv1connect.UnimplementedChoresServiceHandler
	db *sql.DB

	// VAPID keypair used to sign Web Push messages. Empty when key setup
	// failed (see ensureVAPIDKeys), in which case push notifications are
	// silently unavailable rather than fatal to the rest of the app.
	vapidPublicKey  string
	vapidPrivateKey string
}

func New(db *sql.DB) *Server {
	s := &Server{db: db}
	if err := s.ensureVAPIDKeys(); err != nil {
		log.Printf("push notifications disabled: %v", err)
	}
	return s
}

func newID() string {
	return uuid.NewString()
}

// Timestamps are stored as fixed-width RFC3339 UTC strings so that
// lexicographic ordering matches chronological ordering and comparisons
// (e.g. "completed_at >= ?") work directly in SQL.

func nowUTC() time.Time {
	return time.Now().UTC()
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}

func timestampPB(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}

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

// ---- Families -----------------------------------------------------

func (s *Server) CreateFamily(ctx context.Context, req *connect.Request[v1.CreateFamilyRequest]) (*connect.Response[v1.CreateFamilyResponse], error) {
	name := req.Msg.GetName()
	if name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}

	// Creating a family also makes the caller its founding parent, bound to
	// their login identity. Someone who already belongs to a family can't
	// found another one with the same login.
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ?`, identity.Sub).Scan(&existing)
	if err == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you already belong to a family"))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO families (id, name, created_at) VALUES (?, ?, ?)`,
		id, name, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create family: %w", err))
	}

	parentName := req.Msg.GetParentName()
	if parentName == "" {
		parentName = identity.Name
	}
	if parentName == "" {
		parentName = identity.Email
	}
	if parentName == "" {
		parentName = "Parent"
	}
	uid := newID()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, family_id, name, role, created_at, auth_subject, email) VALUES (?, ?, ?, 'parent', ?, ?, ?)`,
		uid, id, parentName, formatTime(now), identity.Sub, identity.Email,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bind founding parent: %w", err))
	}

	return connect.NewResponse(&v1.CreateFamilyResponse{
		Family: &v1.Family{Id: id, Name: name, CreatedAt: timestampPB(now)},
	}), nil
}

func (s *Server) ListFamilies(ctx context.Context, _ *connect.Request[v1.ListFamiliesRequest]) (*connect.Response[v1.ListFamiliesResponse], error) {
	// A login only ever sees the family/families it's bound to.
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT f.id, f.name, f.created_at FROM families f
		 JOIN users u ON u.family_id = f.id
		 WHERE u.auth_subject = ?
		 ORDER BY f.created_at`,
		identity.Sub,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var families []*v1.Family
	for rows.Next() {
		var f v1.Family
		var createdAt string
		if err := rows.Scan(&f.Id, &f.Name, &createdAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		f.CreatedAt = timestampPB(t)
		families = append(families, &f)
	}
	return connect.NewResponse(&v1.ListFamiliesResponse{Families: families}), nil
}

func (s *Server) getFamily(ctx context.Context, familyID string) (*v1.Family, error) {
	var f v1.Family
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, name, created_at FROM families WHERE id = ?`, familyID).
		Scan(&f.Id, &f.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	f.CreatedAt = timestampPB(t)
	return &f, nil
}

// DeleteFamily removes the family and, via cascading foreign keys,
// everything tied to it — users, tasks, completions, payouts, invitations,
// push subscriptions.
func (s *Server) DeleteFamily(ctx context.Context, req *connect.Request[v1.DeleteFamilyRequest]) (*connect.Response[v1.DeleteFamilyResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM families WHERE id = ?`, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete family: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("family not found"))
	}
	return connect.NewResponse(&v1.DeleteFamilyResponse{}), nil
}

func (s *Server) UpdateFamily(ctx context.Context, req *connect.Request[v1.UpdateFamilyRequest]) (*connect.Response[v1.UpdateFamilyResponse], error) {
	familyID := req.Msg.GetFamilyId()
	name := strings.TrimSpace(req.Msg.GetName())
	if familyID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and name are required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE families SET name = ? WHERE id = ?`, name, familyID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update family: %w", err))
	}
	family, err := s.getFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.UpdateFamilyResponse{Family: family}), nil
}

// ---- Users -----------------------------------------------------

func roleToDB(role v1.UserRole) (string, error) {
	switch role {
	case v1.UserRole_USER_ROLE_PARENT:
		return "parent", nil
	case v1.UserRole_USER_ROLE_CHILD:
		return "child", nil
	default:
		return "", fmt.Errorf("invalid role %v", role)
	}
}

func roleFromDB(role string) v1.UserRole {
	switch role {
	case "parent":
		return v1.UserRole_USER_ROLE_PARENT
	case "child":
		return v1.UserRole_USER_ROLE_CHILD
	default:
		return v1.UserRole_USER_ROLE_UNSPECIFIED
	}
}

func iconTypeToDB(t v1.IconType) (string, error) {
	switch t {
	case v1.IconType_ICON_TYPE_EMOJI:
		return "emoji", nil
	case v1.IconType_ICON_TYPE_FONT_AWESOME:
		return "fontawesome", nil
	case v1.IconType_ICON_TYPE_MATERIAL_SYMBOLS:
		return "materialsymbols", nil
	default:
		return "", fmt.Errorf("invalid icon type %v", t)
	}
}

func iconTypeFromDB(t string) v1.IconType {
	switch t {
	case "emoji":
		return v1.IconType_ICON_TYPE_EMOJI
	case "fontawesome":
		return v1.IconType_ICON_TYPE_FONT_AWESOME
	case "materialsymbols":
		return v1.IconType_ICON_TYPE_MATERIAL_SYMBOLS
	default:
		return v1.IconType_ICON_TYPE_UNSPECIFIED
	}
}

// taskIconToDB validates icon (which may be nil, meaning "no icon") and
// returns the (icon_type, icon_value) pair to store.
func taskIconToDB(icon *v1.Icon) (string, string, error) {
	if icon.GetValue() == "" {
		return "", "", nil
	}
	dbType, err := iconTypeToDB(icon.GetType())
	if err != nil {
		return "", "", fmt.Errorf("icon: %w", err)
	}
	return dbType, icon.GetValue(), nil
}

func taskIconFromDB(iconType, iconValue string) *v1.Icon {
	if iconValue == "" {
		return nil
	}
	return &v1.Icon{Type: iconTypeFromDB(iconType), Value: iconValue}
}

func repeatModeToDB(m v1.RepeatMode) (string, error) {
	switch m {
	case v1.RepeatMode_REPEAT_MODE_ONCE:
		return "once", nil
	case v1.RepeatMode_REPEAT_MODE_WEEKLY:
		return "weekly", nil
	case v1.RepeatMode_REPEAT_MODE_CRON:
		return "cron", nil
	default:
		return "", fmt.Errorf("invalid repeat_mode %v", m)
	}
}

func repeatModeFromDB(m string) v1.RepeatMode {
	switch m {
	case "once":
		return v1.RepeatMode_REPEAT_MODE_ONCE
	case "weekly":
		return v1.RepeatMode_REPEAT_MODE_WEEKLY
	case "cron":
		return v1.RepeatMode_REPEAT_MODE_CRON
	default:
		return v1.RepeatMode_REPEAT_MODE_UNSPECIFIED
	}
}

func daysOfWeekToDB(days []int) string {
	if len(days) == 0 {
		return ""
	}
	strs := make([]string, len(days))
	for i, d := range days {
		strs[i] = strconv.Itoa(d)
	}
	return strings.Join(strs, ",")
}

func int32SliceFrom(ints []int) []int32 {
	if len(ints) == 0 {
		return nil
	}
	out := make([]int32, len(ints))
	for i, v := range ints {
		out[i] = int32(v)
	}
	return out
}

func daysOfWeekFromDB(s string) []int32 {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	days := make([]int32, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			continue
		}
		days = append(days, int32(n))
	}
	return days
}

// taskSpecFromFields builds a scheduling.Spec from a task's repeat fields,
// whether they come from an incoming request or from a row already in the
// database. now is only consulted as a default: a WEEKLY task with no
// explicit start_date is anchored to it — needed to compute "every N weeks"
// parity, and harmless when repeat_interval_weeks is 1 since the anchor is
// unused there. A task loaded from storage always has start_date already
// populated (CreateTask/UpdateTask never leave it empty), so this default
// only ever fires for a fresh request.
func taskSpecFromFields(mode v1.RepeatMode, cronSchedule string, daysOfWeek []int32, intervalWeeks int32, startDate string, now time.Time) (scheduling.Spec, error) {
	spec := scheduling.Spec{IntervalWeeks: 1} // meaningless outside ModeWeekly; kept at its natural default rather than left at Go's zero value
	switch mode {
	case v1.RepeatMode_REPEAT_MODE_ONCE:
		spec.Mode = scheduling.ModeOnce
		spec.StartDate = startDate
	case v1.RepeatMode_REPEAT_MODE_WEEKLY:
		spec.Mode = scheduling.ModeWeekly
		days := make([]int, len(daysOfWeek))
		for i, d := range daysOfWeek {
			days[i] = int(d)
		}
		spec.DaysOfWeek = days
		spec.IntervalWeeks = int(intervalWeeks)
		if spec.IntervalWeeks < 1 {
			spec.IntervalWeeks = 1
		}
		spec.StartDate = startDate
		if spec.StartDate == "" {
			spec.StartDate = scheduling.FormatDate(now)
		}
	case v1.RepeatMode_REPEAT_MODE_CRON:
		spec.Mode = scheduling.ModeCron
		spec.Cron = cronSchedule
	default:
		return scheduling.Spec{}, errors.New("repeat_mode is required")
	}
	return spec, nil
}

func taskSpec(t *v1.Task, now time.Time) (scheduling.Spec, error) {
	return taskSpecFromFields(t.GetRepeatMode(), t.GetSchedule(), t.GetDaysOfWeek(), t.GetRepeatIntervalWeeks(), t.GetStartDate(), now)
}

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

// ---- Tasks -----------------------------------------------------

func dedupeStrings(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// validateChildIDs ensures childIDs is non-empty and every id names a child
// belonging to familyID — used whenever a task's assignment is set.
func (s *Server) validateChildIDs(ctx context.Context, familyID string, childIDs []string) error {
	if len(childIDs) == 0 {
		return errors.New("child_ids must include at least one child")
	}
	for _, id := range childIDs {
		var role, fid string
		err := s.db.QueryRowContext(ctx, `SELECT role, family_id FROM users WHERE id = ?`, id).Scan(&role, &fid)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("child %q not found", id)
		}
		if err != nil {
			return err
		}
		if fid != familyID {
			return fmt.Errorf("child %q does not belong to this family", id)
		}
		if role != "child" {
			return fmt.Errorf("user %q is not a child", id)
		}
	}
	return nil
}

// setTaskAssignments replaces a task's full set of assigned children.
func (s *Server) setTaskAssignments(ctx context.Context, taskID string, childIDs []string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM task_assignments WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, id := range childIDs {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO task_assignments (task_id, child_id) VALUES (?, ?)`, taskID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) taskChildIDs(ctx context.Context, taskID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT child_id FROM task_assignments WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// taskAssignmentsByFamily batches the per-task child-id lookup for
// ListTasks, instead of one query per task.
func (s *Server) taskAssignmentsByFamily(ctx context.Context, familyID string) (map[string][]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT ta.task_id, ta.child_id FROM task_assignments ta
		 JOIN tasks t ON t.id = ta.task_id
		 WHERE t.family_id = ?`,
		familyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var taskID, childID string
		if err := rows.Scan(&taskID, &childID); err != nil {
			return nil, err
		}
		result[taskID] = append(result[taskID], childID)
	}
	return result, rows.Err()
}

func (s *Server) CreateTask(ctx context.Context, req *connect.Request[v1.CreateTaskRequest]) (*connect.Response[v1.CreateTaskResponse], error) {
	familyID := req.Msg.GetFamilyId()
	title := req.Msg.GetTitle()
	if familyID == "" || title == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and title are required"))
	}
	if req.Msg.GetPriceCents() < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("price_cents must not be negative"))
	}
	spec, err := taskSpecFromFields(req.Msg.GetRepeatMode(), req.Msg.GetSchedule(), req.Msg.GetDaysOfWeek(), req.Msg.GetRepeatIntervalWeeks(), req.Msg.GetStartDate(), nowUTC())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := spec.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	childIDs := dedupeStrings(req.Msg.GetChildIds())
	if err := s.validateChildIDs(ctx, familyID, childIDs); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	iconType, iconValue, err := taskIconToDB(req.Msg.GetIcon())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	repeatModeDB, err := repeatModeToDB(req.Msg.GetRepeatMode())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, family_id, title, description, price_cents, schedule, active, created_at, icon_type, icon_value, repeat_mode, days_of_week, repeat_interval_weeks, start_date)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
		id, familyID, title, req.Msg.GetDescription(), req.Msg.GetPriceCents(), spec.Cron, formatTime(now), iconType, iconValue,
		repeatModeDB, daysOfWeekToDB(spec.DaysOfWeek), spec.IntervalWeeks, spec.StartDate,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create task: %w", err))
	}
	if err := s.setTaskAssignments(ctx, id, childIDs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign task: %w", err))
	}
	return connect.NewResponse(&v1.CreateTaskResponse{
		Task: &v1.Task{
			Id: id, FamilyId: familyID, Title: title, Description: req.Msg.GetDescription(),
			PriceCents: req.Msg.GetPriceCents(), Schedule: spec.Cron, Active: true, CreatedAt: timestampPB(now),
			ChildIds: childIDs, Icon: taskIconFromDB(iconType, iconValue),
			RepeatMode: req.Msg.GetRepeatMode(), DaysOfWeek: int32SliceFrom(spec.DaysOfWeek),
			RepeatIntervalWeeks: int32(spec.IntervalWeeks), StartDate: spec.StartDate,
		},
	}), nil
}

func (s *Server) UpdateTask(ctx context.Context, req *connect.Request[v1.UpdateTaskRequest]) (*connect.Response[v1.UpdateTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	if taskID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id is required"))
	}
	spec, err := taskSpecFromFields(req.Msg.GetRepeatMode(), req.Msg.GetSchedule(), req.Msg.GetDaysOfWeek(), req.Msg.GetRepeatIntervalWeeks(), req.Msg.GetStartDate(), nowUTC())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := spec.Validate(); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	existing, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.requireParent(ctx, existing.FamilyId); err != nil {
		return nil, err
	}
	childIDs := dedupeStrings(req.Msg.GetChildIds())
	if err := s.validateChildIDs(ctx, existing.FamilyId, childIDs); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	iconType, iconValue, err := taskIconToDB(req.Msg.GetIcon())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	repeatModeDB, err := repeatModeToDB(req.Msg.GetRepeatMode())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, price_cents = ?, schedule = ?, active = ?, icon_type = ?, icon_value = ?,
		 repeat_mode = ?, days_of_week = ?, repeat_interval_weeks = ?, start_date = ? WHERE id = ?`,
		req.Msg.GetTitle(), req.Msg.GetDescription(), req.Msg.GetPriceCents(), spec.Cron, req.Msg.GetActive(), iconType, iconValue,
		repeatModeDB, daysOfWeekToDB(spec.DaysOfWeek), spec.IntervalWeeks, spec.StartDate, taskID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update task: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
	}
	if err := s.setTaskAssignments(ctx, taskID, childIDs); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("assign task: %w", err))
	}

	task, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.UpdateTaskResponse{Task: task}), nil
}

func (s *Server) DeleteTask(ctx context.Context, req *connect.Request[v1.DeleteTaskRequest]) (*connect.Response[v1.DeleteTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	if taskID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id is required"))
	}
	task, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.requireParent(ctx, task.FamilyId); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, taskID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete task: %w", err))
	}
	return connect.NewResponse(&v1.DeleteTaskResponse{}), nil
}

const taskColumns = `id, family_id, title, description, price_cents, schedule, active, created_at, icon_type, icon_value,
	repeat_mode, days_of_week, repeat_interval_weeks, start_date`

func scanTask(row rowScanner) (*v1.Task, error) {
	var t v1.Task
	var createdAt, iconType, iconValue, repeatMode, daysOfWeek string
	var active bool
	var intervalWeeks int32
	if err := row.Scan(&t.Id, &t.FamilyId, &t.Title, &t.Description, &t.PriceCents, &t.Schedule, &active, &createdAt, &iconType, &iconValue,
		&repeatMode, &daysOfWeek, &intervalWeeks, &t.StartDate); err != nil {
		return nil, err
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	t.Active = active
	t.CreatedAt = timestampPB(ts)
	t.Icon = taskIconFromDB(iconType, iconValue)
	t.RepeatMode = repeatModeFromDB(repeatMode)
	t.DaysOfWeek = daysOfWeekFromDB(daysOfWeek)
	t.RepeatIntervalWeeks = intervalWeeks
	return &t, nil
}

func (s *Server) getTask(ctx context.Context, taskID string) (*v1.Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ?`,
		taskID,
	)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	childIDs, err := s.taskChildIDs(ctx, taskID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	t.ChildIds = childIDs
	return t, nil
}

func (s *Server) ListTasks(ctx context.Context, req *connect.Request[v1.ListTasksRequest]) (*connect.Response[v1.ListTasksResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE family_id = ? ORDER BY created_at`,
		familyID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var tasks []*v1.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	assignments, err := s.taskAssignmentsByFamily(ctx, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	for _, t := range tasks {
		t.ChildIds = assignments[t.Id]
	}
	return connect.NewResponse(&v1.ListTasksResponse{Tasks: tasks}), nil
}

func (s *Server) ListTaskOccurrences(ctx context.Context, req *connect.Request[v1.ListTaskOccurrencesRequest]) (*connect.Response[v1.ListTaskOccurrencesResponse], error) {
	familyID := s.resolvedFamilyID(ctx, req.Msg.GetFamilyId())
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if !s.authorizedForDashboard(ctx, familyID) {
		if err := s.requireMembership(ctx, familyID); err != nil {
			return nil, err
		}
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, req.Msg.GetChildId())
	if err != nil {
		return nil, err
	}
	start, err := scheduling.ParseDate(req.Msg.GetStartDate())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid start_date: %w", err))
	}
	end, err := scheduling.ParseDate(req.Msg.GetEndDate())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid end_date: %w", err))
	}

	tasksResp, err := s.ListTasks(ctx, connect.NewRequest(&v1.ListTasksRequest{FamilyId: familyID}))
	if err != nil {
		return nil, err
	}
	usersResp, err := s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: familyID}))
	if err != nil {
		return nil, err
	}
	childNames := make(map[string]string, len(usersResp.Msg.GetUsers()))
	for _, u := range usersResp.Msg.GetUsers() {
		childNames[u.Id] = u.Name
	}

	completions, err := s.listCompletionsByTaskChildDate(ctx, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	now := nowUTC()
	var occurrences []*v1.TaskOccurrence
	for _, t := range tasksResp.Msg.GetTasks() {
		if !t.GetActive() {
			continue
		}
		spec, err := taskSpec(t, now)
		if err != nil {
			continue
		}
		dates, err := spec.DatesBetween(start, end)
		if err != nil {
			continue
		}
		for _, childID := range t.GetChildIds() {
			if childFilter != "" && childID != childFilter {
				continue
			}
			for _, d := range dates {
				dateStr := scheduling.FormatDate(d)
				occ := &v1.TaskOccurrence{
					Task: t, DueDate: dateStr, ChildId: childID, ChildName: childNames[childID],
				}
				if c, ok := completions[completionKey(t.GetId(), childID, dateStr)]; ok {
					occ.Completed = true
					occ.Completion = c
				}
				occurrences = append(occurrences, occ)
			}
		}
	}
	return connect.NewResponse(&v1.ListTaskOccurrencesResponse{Occurrences: occurrences}), nil
}

func completionKey(taskID, childID, dueDate string) string {
	return taskID + "|" + childID + "|" + dueDate
}

func (s *Server) listCompletionsByTaskChildDate(ctx context.Context, familyID string) (map[string]*v1.TaskCompletion, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, child_id, family_id, due_date, amount_cents, completed_at
		 FROM task_completions WHERE family_id = ?`,
		familyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]*v1.TaskCompletion{}
	for rows.Next() {
		c, err := scanCompletion(rows)
		if err != nil {
			return nil, err
		}
		result[completionKey(c.TaskId, c.ChildId, c.DueDate)] = c
	}
	return result, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCompletion(row rowScanner) (*v1.TaskCompletion, error) {
	var c v1.TaskCompletion
	var completedAt string
	if err := row.Scan(&c.Id, &c.TaskId, &c.ChildId, &c.FamilyId, &c.DueDate, &c.AmountCents, &completedAt); err != nil {
		return nil, err
	}
	t, err := parseTime(completedAt)
	if err != nil {
		return nil, err
	}
	c.CompletedAt = timestampPB(t)
	return &c, nil
}

func scanUser(row rowScanner) (*v1.User, error) {
	var u v1.User
	var role, createdAt, email string
	var authSubject sql.NullString
	if err := row.Scan(&u.Id, &u.FamilyId, &u.Name, &role, &createdAt, &email, &authSubject); err != nil {
		return nil, err
	}
	t, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	u.Role = roleFromDB(role)
	u.CreatedAt = timestampPB(t)
	u.Email = email
	u.AuthBound = authSubject.Valid && authSubject.String != ""
	return &u, nil
}

// ---- Task completions -----------------------------------------------------

func (s *Server) CompleteTask(ctx context.Context, req *connect.Request[v1.CompleteTaskRequest]) (*connect.Response[v1.CompleteTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	childID := req.Msg.GetChildId()
	if taskID == "" || childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id and child_id are required"))
	}
	dueDate := req.Msg.GetDueDate()
	if dueDate == "" {
		dueDate = scheduling.FormatDate(time.Now())
	} else if _, err := scheduling.ParseDate(dueDate); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid due_date: %w", err))
	}

	task, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !s.authorizedForDashboard(ctx, task.FamilyId) {
		if err := s.requireMembership(ctx, task.FamilyId); err != nil {
			return nil, err
		}
		if err := s.requireSelfOrParent(ctx, task.FamilyId, childID); err != nil {
			return nil, err
		}
	}

	var childFamilyID, childName string
	if err := s.db.QueryRowContext(ctx, `SELECT family_id, name FROM users WHERE id = ? AND role = 'child'`, childID).Scan(&childFamilyID, &childName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if childFamilyID != task.FamilyId {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child does not belong to the task's family"))
	}
	if !containsString(task.ChildIds, childID) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child is not assigned to this task"))
	}

	id := newID()
	now := nowUTC()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO task_completions (id, task_id, child_id, family_id, due_date, amount_cents, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (task_id, child_id, due_date) DO UPDATE SET amount_cents = excluded.amount_cents`,
		id, taskID, childID, task.FamilyId, dueDate, task.PriceCents, formatTime(now),
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("complete task: %w", err))
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT id, task_id, child_id, family_id, due_date, amount_cents, completed_at
		 FROM task_completions WHERE task_id = ? AND child_id = ? AND due_date = ?`,
		taskID, childID, dueDate,
	)
	completion, err := scanCompletion(row)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	go s.notifyTaskCompleted(task.FamilyId, s.actingUserID(ctx, task.FamilyId), childName, task.Title, task.PriceCents)

	return connect.NewResponse(&v1.CompleteTaskResponse{Completion: completion}), nil
}

func (s *Server) UncompleteTask(ctx context.Context, req *connect.Request[v1.UncompleteTaskRequest]) (*connect.Response[v1.UncompleteTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	childID := req.Msg.GetChildId()
	dueDate := req.Msg.GetDueDate()
	if taskID == "" || childID == "" || dueDate == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id, child_id and due_date are required"))
	}
	task, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if !s.authorizedForDashboard(ctx, task.FamilyId) {
		if err := s.requireMembership(ctx, task.FamilyId); err != nil {
			return nil, err
		}
		if err := s.requireSelfOrParent(ctx, task.FamilyId, childID); err != nil {
			return nil, err
		}
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM task_completions WHERE task_id = ? AND child_id = ? AND due_date = ?`,
		taskID, childID, dueDate,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("uncomplete task: %w", err))
	}
	return connect.NewResponse(&v1.UncompleteTaskResponse{}), nil
}

const (
	defaultCompletionsLimit = 20
	maxCompletionsLimit     = 100
)

func scanCompletionWithNames(row rowScanner) (*v1.TaskCompletion, error) {
	var c v1.TaskCompletion
	var completedAt string
	if err := row.Scan(&c.Id, &c.TaskId, &c.ChildId, &c.FamilyId, &c.DueDate, &c.AmountCents, &completedAt, &c.TaskTitle, &c.ChildName); err != nil {
		return nil, err
	}
	t, err := parseTime(completedAt)
	if err != nil {
		return nil, err
	}
	c.CompletedAt = timestampPB(t)
	return &c, nil
}

func (s *Server) ListTaskCompletions(ctx context.Context, req *connect.Request[v1.ListTaskCompletionsRequest]) (*connect.Response[v1.ListTaskCompletionsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, req.Msg.GetChildId())
	if err != nil {
		return nil, err
	}

	limit := int(req.Msg.GetLimit())
	if limit <= 0 {
		limit = defaultCompletionsLimit
	}
	if limit > maxCompletionsLimit {
		limit = maxCompletionsLimit
	}
	offset := int(req.Msg.GetOffset())
	if offset < 0 {
		offset = 0
	}

	query := `SELECT tc.id, tc.task_id, tc.child_id, tc.family_id, tc.due_date, tc.amount_cents, tc.completed_at, t.title, u.name
	           FROM task_completions tc
	           JOIN tasks t ON t.id = tc.task_id
	           JOIN users u ON u.id = tc.child_id
	           WHERE tc.family_id = ?`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND tc.child_id = ?`
		args = append(args, childFilter)
	}
	if start := req.Msg.GetStartDate(); start != "" {
		query += ` AND tc.due_date >= ?`
		args = append(args, start)
	}
	if end := req.Msg.GetEndDate(); end != "" {
		query += ` AND tc.due_date <= ?`
		args = append(args, end)
	}
	if search := strings.TrimSpace(req.Msg.GetSearch()); search != "" {
		query += ` AND (LOWER(t.title) LIKE '%' || LOWER(?) || '%' OR LOWER(u.name) LIKE '%' || LOWER(?) || '%')`
		args = append(args, search, search)
	}
	query += ` ORDER BY tc.due_date DESC, tc.completed_at DESC LIMIT ? OFFSET ?`
	// Fetch one extra row to know whether another page exists, without a
	// separate COUNT(*) query.
	args = append(args, limit+1, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var completions []*v1.TaskCompletion
	for rows.Next() {
		c, err := scanCompletionWithNames(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		completions = append(completions, c)
	}
	if err := rows.Err(); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	hasMore := len(completions) > limit
	if hasMore {
		completions = completions[:limit]
	}
	return connect.NewResponse(&v1.ListTaskCompletionsResponse{Completions: completions, HasMore: hasMore}), nil
}

// ---- Accounting -----------------------------------------------------

// mondayOfWeek returns the calendar date (midnight, t's own location) of
// the Monday on or before t — the Monday-first week boundary the UI shows
// elsewhere, as opposed to a rolling 7-day window.
func mondayOfWeek(t time.Time) time.Time {
	t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	daysSinceMonday := (int(t.Weekday()) + 6) % 7 // Mon=0 .. Sun=6
	return t.AddDate(0, 0, -daysSinceMonday)
}

func (s *Server) computeSummary(ctx context.Context, child *v1.User) (*v1.ChildSummary, error) {
	var totalEarned, earnedLast7Days, earnedToday, earnedThisWeek, totalPaidOut sql.NullInt64
	sevenDaysAgo := formatTime(nowUTC().AddDate(0, 0, -7))
	today := scheduling.FormatDate(nowUTC())
	startOfWeek := scheduling.FormatDate(mondayOfWeek(nowUTC()))

	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM task_completions WHERE child_id = ?`,
		child.Id,
	).Scan(&totalEarned); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM task_completions WHERE child_id = ? AND completed_at >= ?`,
		child.Id, sevenDaysAgo,
	).Scan(&earnedLast7Days); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM task_completions WHERE child_id = ? AND due_date = ?`,
		child.Id, today,
	).Scan(&earnedToday); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM task_completions WHERE child_id = ? AND due_date >= ?`,
		child.Id, startOfWeek,
	).Scan(&earnedThisWeek); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount_cents), 0) FROM payouts WHERE child_id = ?`,
		child.Id,
	).Scan(&totalPaidOut); err != nil {
		return nil, err
	}

	var lastPayoutAt sql.NullString
	if err := s.db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM payouts WHERE child_id = ?`,
		child.Id,
	).Scan(&lastPayoutAt); err != nil {
		return nil, err
	}

	summary := &v1.ChildSummary{
		Child:                 child,
		EarnedLast_7DaysCents: earnedLast7Days.Int64,
		EarnedTodayCents:      earnedToday.Int64,
		EarnedThisWeekCents:   earnedThisWeek.Int64,
		TotalEarnedCents:      totalEarned.Int64,
		TotalPaidOutCents:     totalPaidOut.Int64,
		BalanceCents:          totalEarned.Int64 - totalPaidOut.Int64,
	}
	if lastPayoutAt.Valid {
		t, err := parseTime(lastPayoutAt.String)
		if err != nil {
			return nil, err
		}
		summary.LastPayoutAt = timestampPB(t)
	}
	return summary, nil
}

func (s *Server) getUser(ctx context.Context, userID string) (*v1.User, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, family_id, name, role, created_at, email, auth_subject FROM users WHERE id = ?`,
		userID,
	)
	u, err := scanUser(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("user not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return u, nil
}

func (s *Server) GetChildSummary(ctx context.Context, req *connect.Request[v1.GetChildSummaryRequest]) (*connect.Response[v1.GetChildSummaryResponse], error) {
	childID := req.Msg.GetChildId()
	if childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child_id is required"))
	}
	child, err := s.getUser(ctx, childID)
	if err != nil {
		return nil, err
	}
	if err := s.requireMembership(ctx, child.FamilyId); err != nil {
		return nil, err
	}
	if err := s.requireSelfOrParent(ctx, child.FamilyId, childID); err != nil {
		return nil, err
	}
	summary, err := s.computeSummary(ctx, child)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetChildSummaryResponse{Summary: summary}), nil
}

func (s *Server) ListChildSummaries(ctx context.Context, req *connect.Request[v1.ListChildSummariesRequest]) (*connect.Response[v1.ListChildSummariesResponse], error) {
	familyID := s.resolvedFamilyID(ctx, req.Msg.GetFamilyId())
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if !s.authorizedForDashboard(ctx, familyID) {
		if err := s.requireMembership(ctx, familyID); err != nil {
			return nil, err
		}
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, "")
	if err != nil {
		return nil, err
	}
	query := `SELECT id, family_id, name, role, created_at, email, auth_subject FROM users WHERE family_id = ? AND role = 'child'`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND id = ?`
		args = append(args, childFilter)
	}
	query += ` ORDER BY created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var children []*v1.User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		children = append(children, u)
	}

	var summaries []*v1.ChildSummary
	for _, c := range children {
		summary, err := s.computeSummary(ctx, c)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		summaries = append(summaries, summary)
	}
	return connect.NewResponse(&v1.ListChildSummariesResponse{Summaries: summaries}), nil
}

// ---- Payouts -----------------------------------------------------

func (s *Server) CreatePayout(ctx context.Context, req *connect.Request[v1.CreatePayoutRequest]) (*connect.Response[v1.CreatePayoutResponse], error) {
	childID := req.Msg.GetChildId()
	if childID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child_id is required"))
	}
	child, err := s.getUser(ctx, childID)
	if err != nil {
		return nil, err
	}
	if err := s.requireParent(ctx, child.FamilyId); err != nil {
		return nil, err
	}
	if child.Role != v1.UserRole_USER_ROLE_CHILD {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("payouts can only be made to children"))
	}

	amount := req.Msg.GetAmountCents()
	if req.Msg.GetFullPayout() {
		summary, err := s.computeSummary(ctx, child)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		amount = summary.BalanceCents
	}
	if amount <= 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("amount_cents must be greater than zero"))
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO payouts (id, child_id, family_id, amount_cents, full_payout, note, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, childID, child.FamilyId, amount, req.Msg.GetFullPayout(), req.Msg.GetNote(), formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create payout: %w", err))
	}

	return connect.NewResponse(&v1.CreatePayoutResponse{
		Payout: &v1.Payout{
			Id: id, ChildId: childID, FamilyId: child.FamilyId, AmountCents: amount,
			FullPayout: req.Msg.GetFullPayout(), Note: req.Msg.GetNote(), CreatedAt: timestampPB(now),
		},
	}), nil
}

func (s *Server) ListPayouts(ctx context.Context, req *connect.Request[v1.ListPayoutsRequest]) (*connect.Response[v1.ListPayoutsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	childFilter, err := s.selfFilterForChild(ctx, familyID, req.Msg.GetChildId())
	if err != nil {
		return nil, err
	}
	query := `SELECT id, child_id, family_id, amount_cents, full_payout, note, created_at FROM payouts WHERE family_id = ?`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND child_id = ?`
		args = append(args, childFilter)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var payouts []*v1.Payout
	for rows.Next() {
		var p v1.Payout
		var createdAt string
		if err := rows.Scan(&p.Id, &p.ChildId, &p.FamilyId, &p.AmountCents, &p.FullPayout, &p.Note, &createdAt); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		t, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		p.CreatedAt = timestampPB(t)
		payouts = append(payouts, &p)
	}
	return connect.NewResponse(&v1.ListPayoutsResponse{Payouts: payouts}), nil
}

// ---- Membership & invitations -----------------------------------------------------

const invitationTTL = 7 * 24 * time.Hour

// GetMyMembership returns every user row the caller's login identity is
// bound to. Usually that's at most one (a parent belongs to one household),
// but a child can be bound to several — e.g. one per household they split
// time between. A freshly logged-in identity with no matching rows yet
// (hasn't created or joined a family) legitimately reports Bound: false.
func (s *Server) GetMyMembership(ctx context.Context, _ *connect.Request[v1.GetMyMembershipRequest]) (*connect.Response[v1.GetMyMembershipResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	rows, err := s.db.QueryContext(ctx, `SELECT id FROM users WHERE auth_subject = ? ORDER BY created_at`, identity.Sub)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var userIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		userIDs = append(userIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rows.Close()

	if len(userIDs) == 0 {
		return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: false}), nil
	}

	memberships := make([]*v1.Membership, 0, len(userIDs))
	for _, userID := range userIDs {
		user, err := s.getUser(ctx, userID)
		if err != nil {
			return nil, err
		}
		family, err := s.getFamily(ctx, user.FamilyId)
		if err != nil {
			return nil, err
		}
		memberships = append(memberships, &v1.Membership{User: user, Family: family})
	}
	return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: true, Memberships: memberships}), nil
}

// CreateInvitation creates an unclaimed parent slot in the family and an
// unguessable one-time token that binds a login identity to it once
// accepted. The token is only ever returned here — ListInvitations omits it.
func (s *Server) CreateInvitation(ctx context.Context, req *connect.Request[v1.CreateInvitationRequest]) (*connect.Response[v1.CreateInvitationResponse], error) {
	familyID := req.Msg.GetFamilyId()
	name := req.Msg.GetName()
	if familyID == "" || name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id and name are required"))
	}
	roleStr, err := roleToDB(req.Msg.GetRole())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("role must be parent or child: %w", err))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}

	now := nowUTC()
	uid := newID()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO users (id, family_id, name, role, created_at, email) VALUES (?, ?, ?, ?, ?, ?)`,
		uid, familyID, name, roleStr, formatTime(now), req.Msg.GetEmail(),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create invited slot: %w", err))
	}

	token := newID()
	invID := newID()
	expiresAt := now.Add(invitationTTL)
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO invitations (id, family_id, user_id, email, token, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		invID, familyID, uid, req.Msg.GetEmail(), token, formatTime(now), formatTime(expiresAt),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create invitation: %w", err))
	}

	return connect.NewResponse(&v1.CreateInvitationResponse{
		Invitation: &v1.Invitation{
			Id: invID, FamilyId: familyID, UserId: uid, UserName: name, Email: req.Msg.GetEmail(),
			CreatedAt: timestampPB(now), ExpiresAt: timestampPB(expiresAt), Role: req.Msg.GetRole(),
			Token: token,
		},
		Token:      token,
		AcceptPath: "/invite/accept?token=" + token,
	}), nil
}

func (s *Server) ListInvitations(ctx context.Context, req *connect.Request[v1.ListInvitationsRequest]) (*connect.Response[v1.ListInvitationsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT i.id, i.family_id, i.user_id, u.name, u.role, i.email, i.created_at, i.expires_at, i.accepted_at, i.token
		 FROM invitations i JOIN users u ON u.id = i.user_id
		 WHERE i.family_id = ? ORDER BY i.created_at DESC`,
		familyID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var invitations []*v1.Invitation
	for rows.Next() {
		var inv v1.Invitation
		var role, createdAt, expiresAt, token string
		var acceptedAt sql.NullString
		if err := rows.Scan(&inv.Id, &inv.FamilyId, &inv.UserId, &inv.UserName, &role, &inv.Email, &createdAt, &expiresAt, &acceptedAt, &token); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		inv.Role = roleFromDB(role)
		ct, err := parseTime(createdAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		et, err := parseTime(expiresAt)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		inv.CreatedAt = timestampPB(ct)
		inv.ExpiresAt = timestampPB(et)
		if acceptedAt.Valid {
			at, err := parseTime(acceptedAt.String)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			inv.AcceptedAt = timestampPB(at)
		} else {
			// Only a still-pending invitation's token is any use to anyone —
			// once accepted it can never bind another login, so it's left
			// out rather than needlessly exposing a spent credential.
			inv.Token = token
		}
		invitations = append(invitations, &inv)
	}
	return connect.NewResponse(&v1.ListInvitationsResponse{Invitations: invitations}), nil
}

// RevokeInvitation deletes a not-yet-accepted invitation along with the
// unclaimed parent slot it created.
func (s *Server) RevokeInvitation(ctx context.Context, req *connect.Request[v1.RevokeInvitationRequest]) (*connect.Response[v1.RevokeInvitationResponse], error) {
	invID := req.Msg.GetInvitationId()
	if invID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("invitation_id is required"))
	}

	var familyID, userID string
	var acceptedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT family_id, user_id, accepted_at FROM invitations WHERE id = ?`, invID).
		Scan(&familyID, &userID, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invitation not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}
	if acceptedAt.Valid {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation was already accepted"))
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM invitations WHERE id = ?`, invID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("revoke invitation: %w", err))
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ? AND auth_subject IS NULL`, userID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("remove unclaimed slot: %w", err))
	}
	return connect.NewResponse(&v1.RevokeInvitationResponse{}), nil
}

// AcceptInvitation binds the caller's login identity to the invitation's
// parent slot. Possession of the token is what grants the claim — it isn't
// matched against any particular email address, since a login's verified
// email may differ from whatever address the invite was shared with.
func (s *Server) AcceptInvitation(ctx context.Context, req *connect.Request[v1.AcceptInvitationRequest]) (*connect.Response[v1.AcceptInvitationResponse], error) {
	token := req.Msg.GetToken()
	if token == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("token is required"))
	}
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("login required"))
	}

	var invID, familyID, userID, expiresAtStr string
	var acceptedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT i.id, i.family_id, i.user_id, i.expires_at, i.accepted_at
		 FROM invitations i
		 WHERE i.token = ?`, token,
	).Scan(&invID, &familyID, &userID, &expiresAtStr, &acceptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("invitation not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if acceptedAt.Valid {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation has already been used"))
	}
	expiresAt, err := parseTime(expiresAtStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if time.Now().After(expiresAt) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation has expired"))
	}

	// A login can be bound to the same family only once — otherwise both
	// parents and children can belong to as many families as they've been
	// invited into (e.g. a parent co-running two households, or a child
	// split between them).
	var existing string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ? AND family_id = ?`, identity.Sub, familyID).Scan(&existing)
	if err == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you are already a member of this family"))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE users SET auth_subject = ?, email = ? WHERE id = ? AND auth_subject IS NULL`,
		identity.Sub, identity.Email, userID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("bind invited parent: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitation slot has already been claimed"))
	}

	if _, err := s.db.ExecContext(ctx, `UPDATE invitations SET accepted_at = ? WHERE id = ?`, formatTime(nowUTC()), invID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mark invitation accepted: %w", err))
	}

	user, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	family, err := s.getFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.AcceptInvitationResponse{User: user, Family: family}), nil
}
