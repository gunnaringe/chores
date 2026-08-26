package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
)

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
func (s *Server) setTaskAssignments(ctx context.Context, q querier, taskID string, childIDs []string) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM task_assignments WHERE task_id = ?`, taskID); err != nil {
		return err
	}
	for _, id := range childIDs {
		if _, err := q.ExecContext(ctx, `INSERT INTO task_assignments (task_id, child_id) VALUES (?, ?)`, taskID, id); err != nil {
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
	priceCents := req.Msg.GetPrice().GetCents()
	if priceCents < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("price must not be negative"))
	}
	spec, err := specFromSchedule(req.Msg.GetSchedule(), nowUTC())
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
	id := newID()
	now := nowUTC()
	// One step: a task whose assignments didn't land is assigned to nobody,
	// which means it shows up for nobody and is invisible in every view
	// except the parent's task list.
	if err := s.inTx(ctx, func(q querier) error {
		if _, err := q.ExecContext(ctx,
			`INSERT INTO tasks (id, family_id, title, description, price_cents, schedule, active, created_at, icon_type, icon_value, repeat_mode, days_of_week, repeat_interval_weeks, start_date)
			 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)`,
			id, familyID, title, req.Msg.GetDescription(), priceCents, spec.Cron, formatTime(now), iconType, iconValue,
			string(spec.Mode), daysOfWeekToDB(spec.DaysOfWeek), spec.IntervalWeeks, spec.StartDate,
		); err != nil {
			return fmt.Errorf("create task: %w", err)
		}
		return s.setTaskAssignments(ctx, q, id, childIDs)
	}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&v1.CreateTaskResponse{
		Task: &v1.Task{
			Id: id, FamilyId: familyID, Title: title, Description: req.Msg.GetDescription(),
			Price: money(priceCents), Schedule: scheduleFromSpec(spec), Active: true, CreatedAt: timestampPB(now),
			ChildIds: childIDs, Icon: taskIconFromDB(iconType, iconValue),
		},
	}), nil
}

func (s *Server) UpdateTask(ctx context.Context, req *connect.Request[v1.UpdateTaskRequest]) (*connect.Response[v1.UpdateTaskResponse], error) {
	taskID := req.Msg.GetTaskId()
	if taskID == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("task_id is required"))
	}
	priceCents := req.Msg.GetPrice().GetCents()
	if priceCents < 0 {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("price must not be negative"))
	}
	spec, err := specFromSchedule(req.Msg.GetSchedule(), nowUTC())
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
	// Freezing what the task has already produced and applying the edit are
	// one atomic step. Half of it — history pinned against a task that then
	// failed to change, or worse, a change applied over history that was
	// never pinned — would be a silent, permanent misstatement of what a
	// child earned.
	notFound := errors.New("task not found")
	freeze := restatesHistory(existing, req.Msg)
	err = s.inTx(ctx, func(q querier) error {
		if freeze {
			if err := s.freezeOccurrences(ctx, q, existing, nowUTC()); err != nil {
				return fmt.Errorf("freeze occurrences: %w", err)
			}
		}
		res, err := q.ExecContext(ctx,
			`UPDATE tasks SET title = ?, description = ?, price_cents = ?, schedule = ?, active = ?, icon_type = ?, icon_value = ?,
			 repeat_mode = ?, days_of_week = ?, repeat_interval_weeks = ?, start_date = ? WHERE id = ? AND deleted_at IS NULL`,
			req.Msg.GetTitle(), req.Msg.GetDescription(), priceCents, spec.Cron, req.Msg.GetActive(), iconType, iconValue,
			string(spec.Mode), daysOfWeekToDB(spec.DaysOfWeek), spec.IntervalWeeks, spec.StartDate, taskID,
		)
		if err != nil {
			return fmt.Errorf("update task: %w", err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return notFound
		}
		if err := s.setTaskAssignments(ctx, q, taskID, childIDs); err != nil {
			return fmt.Errorf("assign task: %w", err)
		}
		return nil
	})
	if errors.Is(err, notFound) {
		return nil, connect.NewError(connect.CodeNotFound, notFound)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
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
	// Soft delete. The row stays so the occurrences it already produced
	// keep rendering and the earnings behind them keep counting — a hard
	// delete used to cascade both away, driving the balance of a child
	// who'd already been paid negative. deleted_at doubles as the cutoff
	// for schedule expansion: see occurrenceCutoff.
	//
	// Deliberately does not also clear `active`: that flag means "paused",
	// and a paused task generates no occurrences at all, past ones
	// included. Setting it here would suppress exactly the history this
	// change exists to keep.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET deleted_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(nowUTC()), taskID,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("delete task: %w", err))
	}
	return connect.NewResponse(&v1.DeleteTaskResponse{}), nil
}

const taskColumns = `id, family_id, title, description, price_cents, schedule, active, created_at, icon_type, icon_value,
	repeat_mode, days_of_week, repeat_interval_weeks, start_date, deleted_at`

func scanTask(row rowScanner) (*v1.Task, error) {
	var t v1.Task
	var createdAt, iconType, iconValue, repeatMode, daysOfWeek, startDate, cronExpr string
	var active bool
	var priceCents int64
	var intervalWeeks int32
	var deletedAt sql.NullString
	if err := row.Scan(&t.Id, &t.FamilyId, &t.Title, &t.Description, &priceCents, &cronExpr, &active, &createdAt, &iconType, &iconValue,
		&repeatMode, &daysOfWeek, &intervalWeeks, &startDate, &deletedAt); err != nil {
		return nil, err
	}
	ts, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	t.Active = active
	t.Price = money(priceCents)
	t.CreatedAt = timestampPB(ts)
	t.Icon = taskIconFromDB(iconType, iconValue)
	t.Schedule = scheduleFromSpec(specFromDB(repeatMode, cronExpr, daysOfWeek, intervalWeeks, startDate))
	if deletedAt.Valid && deletedAt.String != "" {
		deleted, err := parseTime(deletedAt.String)
		if err != nil {
			return nil, err
		}
		t.DeletedAt = timestampPB(deleted)
	}
	return &t, nil
}

// getTask loads a live task. A deleted one reports NotFound, which is what
// every caller here wants: it can't be edited, deleted again, or completed
// against. Occurrence expansion, which does need deleted tasks, goes
// through listTasksForOccurrences instead.
func (s *Server) getTask(ctx context.Context, taskID string) (*v1.Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE id = ? AND deleted_at IS NULL`,
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
		`SELECT `+taskColumns+` FROM tasks WHERE family_id = ? AND deleted_at IS NULL ORDER BY created_at`,
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
		t.ChildIds = assignments[t.GetId()]
	}

	return connect.NewResponse(&v1.ListTasksResponse{Tasks: tasks}), nil
}
