// Package server implements the UkelonnService Connect API on top of SQLite.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	v1 "github.com/gunnaringe/chores/gen/ukelonn/v1"
	"github.com/gunnaringe/chores/gen/ukelonn/v1/ukelonnv1connect"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/scheduling"
)

type Server struct {
	ukelonnv1connect.UnimplementedUkelonnServiceHandler
	db *sql.DB
}

func New(db *sql.DB) *Server {
	return &Server{db: db}
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

// currentIdentity returns the caller's login identity, when auth is
// enabled and they're logged in. In local-testing mode (no auth
// configured) it always reports false, which every access check below
// treats as "no restriction" — preserving the original open-access
// behavior when auth isn't in play.
func (s *Server) currentIdentity(ctx context.Context) (auth.Identity, bool) {
	return auth.FromContext(ctx)
}

// boundUser returns the user row the caller's login identity is bound to,
// or nil if it isn't bound to any (which requireRole/requireMembership
// treat as "no family membership").
func (s *Server) boundUser(ctx context.Context, identity auth.Identity) (*v1.User, error) {
	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ?`, identity.Sub).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return s.getUser(ctx, userID)
}

// requireMembership ensures the caller is bound to a user row belonging to
// familyID, regardless of role. A no-op in local-testing mode.
func (s *Server) requireMembership(ctx context.Context, familyID string) error {
	return s.requireRole(ctx, familyID)
}

// requireRole ensures the caller is bound to familyID and, if any roles are
// given, that their role is one of them. A no-op in local-testing mode.
// Used to keep family-management actions (adding members, managing tasks,
// inviting people, paying out) restricted to parents now that children can
// have their own login and API access.
func (s *Server) requireRole(ctx context.Context, familyID string, allowed ...v1.UserRole) error {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil
	}
	user, err := s.boundUser(ctx, identity)
	if err != nil {
		return err
	}
	if user == nil {
		return connect.NewError(connect.CodePermissionDenied, errors.New("you don't belong to a family yet"))
	}
	if user.FamilyId != familyID {
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
// child_id — a bound parent, or local-testing mode, is unrestricted.
// Callers should also call requireMembership/requireParent for the
// relevant family first.
func (s *Server) requireSelfOrParent(ctx context.Context, childID string) error {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return nil
	}
	user, err := s.boundUser(ctx, identity)
	if err != nil {
		return err
	}
	if user != nil && user.Role == v1.UserRole_USER_ROLE_CHILD && user.Id != childID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("children can only act on their own behalf"))
	}
	return nil
}

// selfFilterForChild returns the child_id filter a list RPC should use: a
// bound child is always forced to see only their own data, overriding
// whatever was requested. Anyone else (a bound parent, or local-testing
// mode) gets requested back unchanged.
func (s *Server) selfFilterForChild(ctx context.Context, requested string) (string, error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return requested, nil
	}
	user, err := s.boundUser(ctx, identity)
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

	// When auth is enabled, creating a family also makes the caller its
	// founding parent, bound to their login identity. Someone who already
	// belongs to a family can't found another one with the same login.
	identity, hasIdentity := s.currentIdentity(ctx)
	if hasIdentity {
		var existing string
		err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ?`, identity.Sub).Scan(&existing)
		if err == nil {
			return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you already belong to a family"))
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO families (id, name, created_at) VALUES (?, ?, ?)`,
		id, name, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create family: %w", err))
	}

	if hasIdentity {
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
	}

	return connect.NewResponse(&v1.CreateFamilyResponse{
		Family: &v1.Family{Id: id, Name: name, CreatedAt: timestampPB(now)},
	}), nil
}

func (s *Server) ListFamilies(ctx context.Context, _ *connect.Request[v1.ListFamiliesRequest]) (*connect.Response[v1.ListFamiliesResponse], error) {
	// In local-testing mode, list every family (there's no identity to scope
	// by). Once auth is enabled, a login only ever sees the single family
	// it's bound to.
	query := `SELECT f.id, f.name, f.created_at FROM families f`
	var args []any
	if identity, ok := s.currentIdentity(ctx); ok {
		query += ` JOIN users u ON u.family_id = f.id WHERE u.auth_subject = ?`
		args = append(args, identity.Sub)
	}
	query += ` ORDER BY f.created_at`

	rows, err := s.db.QueryContext(ctx, query, args...)
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

// ---- Tasks -----------------------------------------------------

func (s *Server) CreateTask(ctx context.Context, req *connect.Request[v1.CreateTaskRequest]) (*connect.Response[v1.CreateTaskResponse], error) {
	familyID := req.Msg.GetFamilyId()
	title := req.Msg.GetTitle()
	schedule := req.Msg.GetSchedule()
	if familyID == "" || title == "" || schedule == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id, title and schedule are required"))
	}
	if req.Msg.GetPriceCents() < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("price_cents must not be negative"))
	}
	if err := scheduling.Validate(schedule); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err := s.requireParent(ctx, familyID); err != nil {
		return nil, err
	}

	id := newID()
	now := nowUTC()
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (id, family_id, title, description, price_cents, schedule, active, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?)`,
		id, familyID, title, req.Msg.GetDescription(), req.Msg.GetPriceCents(), schedule, formatTime(now),
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("create task: %w", err))
	}
	return connect.NewResponse(&v1.CreateTaskResponse{
		Task: &v1.Task{
			Id: id, FamilyId: familyID, Title: title, Description: req.Msg.GetDescription(),
			PriceCents: req.Msg.GetPriceCents(), Schedule: schedule, Active: true, CreatedAt: timestampPB(now),
		},
	}), nil
}

func (s *Server) UpdateTask(ctx context.Context, req *connect.Request[v1.UpdateTaskRequest]) (*connect.Response[v1.UpdateTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	if taskID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id is required"))
	}
	if req.Msg.GetSchedule() != "" {
		if err := scheduling.Validate(req.Msg.GetSchedule()); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}

	existing, err := s.getTask(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if err := s.requireParent(ctx, existing.FamilyId); err != nil {
		return nil, err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET title = ?, description = ?, price_cents = ?, schedule = ?, active = ? WHERE id = ?`,
		req.Msg.GetTitle(), req.Msg.GetDescription(), req.Msg.GetPriceCents(), req.Msg.GetSchedule(), req.Msg.GetActive(), taskID,
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("update task: %w", err))
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
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

func scanTask(row rowScanner) (*v1.Task, error) {
	var t v1.Task
	var createdAt string
	var active bool
	if err := row.Scan(&t.Id, &t.FamilyId, &t.Title, &t.Description, &t.PriceCents, &t.Schedule, &active, &createdAt); err != nil {
		return nil, err
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	t.Active = active
	t.CreatedAt = timestampPB(ts)
	return &t, nil
}

func (s *Server) getTask(ctx context.Context, taskID string) (*v1.Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, family_id, title, description, price_cents, schedule, active, created_at FROM tasks WHERE id = ?`,
		taskID,
	)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("task not found"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
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
		`SELECT id, family_id, title, description, price_cents, schedule, active, created_at
		 FROM tasks WHERE family_id = ? ORDER BY created_at`,
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
	return connect.NewResponse(&v1.ListTasksResponse{Tasks: tasks}), nil
}

func (s *Server) ListTaskOccurrences(ctx context.Context, req *connect.Request[v1.ListTaskOccurrencesRequest]) (*connect.Response[v1.ListTaskOccurrencesResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
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

	completions, err := s.listCompletionsByTaskAndDate(ctx, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	var occurrences []*v1.TaskOccurrence
	for _, t := range tasksResp.Msg.GetTasks() {
		if !t.GetActive() {
			continue
		}
		dates, err := scheduling.DatesBetween(t.GetSchedule(), start, end)
		if err != nil {
			continue
		}
		for _, d := range dates {
			dateStr := scheduling.FormatDate(d)
			occ := &v1.TaskOccurrence{Task: t, DueDate: dateStr}
			if c, ok := completions[completionKey(t.GetId(), dateStr)]; ok {
				occ.Completed = true
				occ.Completion = c
			}
			occurrences = append(occurrences, occ)
		}
	}
	return connect.NewResponse(&v1.ListTaskOccurrencesResponse{Occurrences: occurrences}), nil
}

func completionKey(taskID, dueDate string) string {
	return taskID + "|" + dueDate
}

func (s *Server) listCompletionsByTaskAndDate(ctx context.Context, familyID string) (map[string]*v1.TaskCompletion, error) {
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
		result[completionKey(c.TaskId, c.DueDate)] = c
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
	if err := s.requireMembership(ctx, task.FamilyId); err != nil {
		return nil, err
	}
	if err := s.requireSelfOrParent(ctx, childID); err != nil {
		return nil, err
	}

	var childFamilyID string
	if err := s.db.QueryRowContext(ctx, `SELECT family_id FROM users WHERE id = ? AND role = 'child'`, childID).Scan(&childFamilyID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if childFamilyID != task.FamilyId {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("child does not belong to the task's family"))
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
	if err := s.requireMembership(ctx, task.FamilyId); err != nil {
		return nil, err
	}
	if err := s.requireSelfOrParent(ctx, childID); err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM task_completions WHERE task_id = ? AND child_id = ? AND due_date = ?`,
		taskID, childID, dueDate,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("uncomplete task: %w", err))
	}
	return connect.NewResponse(&v1.UncompleteTaskResponse{}), nil
}

func (s *Server) ListTaskCompletions(ctx context.Context, req *connect.Request[v1.ListTaskCompletionsRequest]) (*connect.Response[v1.ListTaskCompletionsResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	childFilter, err := s.selfFilterForChild(ctx, req.Msg.GetChildId())
	if err != nil {
		return nil, err
	}
	query := `SELECT id, task_id, child_id, family_id, due_date, amount_cents, completed_at
	           FROM task_completions WHERE family_id = ?`
	args := []any{familyID}
	if childFilter != "" {
		query += ` AND child_id = ?`
		args = append(args, childFilter)
	}
	query += ` ORDER BY due_date DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	defer rows.Close()

	var completions []*v1.TaskCompletion
	for rows.Next() {
		c, err := scanCompletion(rows)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		completions = append(completions, c)
	}
	return connect.NewResponse(&v1.ListTaskCompletionsResponse{Completions: completions}), nil
}

// ---- Accounting -----------------------------------------------------

func (s *Server) computeSummary(ctx context.Context, child *v1.User) (*v1.ChildSummary, error) {
	var totalEarned, earnedLast7Days, totalPaidOut sql.NullInt64
	sevenDaysAgo := formatTime(nowUTC().AddDate(0, 0, -7))

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
	if err := s.requireSelfOrParent(ctx, childID); err != nil {
		return nil, err
	}
	summary, err := s.computeSummary(ctx, child)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.GetChildSummaryResponse{Summary: summary}), nil
}

func (s *Server) ListChildSummaries(ctx context.Context, req *connect.Request[v1.ListChildSummariesRequest]) (*connect.Response[v1.ListChildSummariesResponse], error) {
	familyID := req.Msg.GetFamilyId()
	if familyID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("family_id is required"))
	}
	if err := s.requireMembership(ctx, familyID); err != nil {
		return nil, err
	}
	childFilter, err := s.selfFilterForChild(ctx, "")
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
	childFilter, err := s.selfFilterForChild(ctx, req.Msg.GetChildId())
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

// GetMyMembership resolves the caller's login identity to the family
// member it's bound to, if any. In local-testing mode it always reports
// unbound, since there is no login identity to resolve.
func (s *Server) GetMyMembership(ctx context.Context, _ *connect.Request[v1.GetMyMembershipRequest]) (*connect.Response[v1.GetMyMembershipResponse], error) {
	identity, ok := s.currentIdentity(ctx)
	if !ok {
		return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: false}), nil
	}

	var userID string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ?`, identity.Sub).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: false}), nil
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	user, err := s.getUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	family, err := s.getFamily(ctx, user.FamilyId)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.GetMyMembershipResponse{Bound: true, User: user, Family: family}), nil
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
	if _, ok := s.currentIdentity(ctx); !ok {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitations require auth0 login to be enabled"))
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
		`SELECT i.id, i.family_id, i.user_id, u.name, u.role, i.email, i.created_at, i.expires_at, i.accepted_at
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
		var role, createdAt, expiresAt string
		var acceptedAt sql.NullString
		if err := rows.Scan(&inv.Id, &inv.FamilyId, &inv.UserId, &inv.UserName, &role, &inv.Email, &createdAt, &expiresAt, &acceptedAt); err != nil {
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
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("invitations require auth0 login to be enabled"))
	}

	var invID, familyID, userID, expiresAtStr string
	var acceptedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, family_id, user_id, expires_at, accepted_at FROM invitations WHERE token = ?`, token,
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

	var existing string
	err = s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE auth_subject = ?`, identity.Sub).Scan(&existing)
	if err == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("you already belong to a family"))
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
