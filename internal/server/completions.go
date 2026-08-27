package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

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
	priceCents := task.GetPrice().GetCents()
	// The task's title, description, icon and price are copied onto the row
	// here and never revisited: this is the moment the occurrence becomes
	// history, and history has to keep saying what it said.
	//
	// On conflict, only completed_at is set, and only when the row isn't
	// already complete. The row may exist for either of two reasons and
	// both are handled by that: it was frozen by an earlier task edit (so
	// it needs completing, at the amount it was frozen at, not the task's
	// current price), or it is already a completion and this is a duplicate
	// submit (so nothing should change at all).
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO task_occurrences (id, task_id, child_id, family_id, due_date, amount_cents, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (task_id, child_id, due_date) DO UPDATE SET completed_at = excluded.completed_at
		 WHERE task_occurrences.completed_at IS NULL`,
		id, taskID, childID, task.FamilyId, dueDate, priceCents, formatTime(now),
	)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("complete task: %w", err))
	}

	row := s.db.QueryRowContext(ctx,
		`SELECT `+occurrenceColumns+` FROM task_occurrences WHERE task_id = ? AND child_id = ? AND due_date = ?`,
		taskID, childID, dueDate,
	)
	occurrence, err := scanOccurrence(row)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	labelOccurrence(occurrence, task, childName)

	go s.notifyTaskCompleted(task.FamilyId, s.actingUserID(ctx, task.FamilyId), childName, task.GetTitle(), priceCents)

	return connect.NewResponse(&v1.CompleteTaskResponse{Occurrence: occurrence}), nil
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
	// Clears the completion but keeps the row, rather than deleting it. The
	// row may have been frozen by an earlier task edit, in which case it
	// carries what this occurrence was worth on the day and deleting it
	// would hand that back to the task's current price. Keeping it also
	// means un-ticking and re-ticking a chore can't change what it pays.
	if _, err := s.db.ExecContext(ctx,
		`UPDATE task_occurrences SET completed_at = NULL WHERE task_id = ? AND child_id = ? AND due_date = ?`,
		taskID, childID, dueDate,
	); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("uncomplete task: %w", err))
	}
	return connect.NewResponse(&v1.UncompleteTaskResponse{}), nil
}

// Upper bound on a page of occurrences, applied when the caller asks for
// one. An unlimited request is still allowed (History's "all time" search
// relies on it); this only caps what an explicit limit can ask for.
const maxOccurrencesLimit = 100
