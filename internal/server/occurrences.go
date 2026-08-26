package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

// Occurrences: the unit the app actually deals in. An occurrence exists as
// a stored row once something has been recorded about it, and is otherwise
// derived from its task's schedule — see the TaskOccurrence proto.

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

	endStr := req.Msg.GetEndDate()
	if endStr == "" {
		endStr = scheduling.FormatDate(nowUTC())
	}
	startStr := req.Msg.GetStartDate()
	if startStr == "" {
		startStr, err = s.occurrenceFloorDate(ctx, familyID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	start, err := scheduling.ParseDate(startStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid start_date: %w", err))
	}
	end, err := scheduling.ParseDate(endStr)
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("invalid end_date: %w", err))
	}

	// Deleted tasks included: one still owns every occurrence it produced
	// before it went, and its schedule is what reconstructs the ones nobody
	// completed.
	tasks, err := s.listTasksForOccurrences(ctx, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	usersResp, err := s.ListUsers(ctx, connect.NewRequest(&v1.ListUsersRequest{FamilyId: familyID}))
	if err != nil {
		return nil, err
	}
	childNames := make(map[string]string, len(usersResp.Msg.GetUsers()))
	for _, u := range usersResp.Msg.GetUsers() {
		childNames[u.GetId()] = u.GetName()
	}

	stored, err := s.listStoredOccurrences(ctx, familyID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	now := nowUTC()
	// Tracks which (task, child, date) keys the schedule loop below already
	// produced, so the second pass over stored rows only adds what it missed.
	seen := make(map[string]bool, len(stored))
	var occurrences []*v1.TaskOccurrence
	for _, t := range tasks {
		// A paused task generates nothing, past dates included. Unchanged
		// behaviour, and it's also why DeleteTask leaves `active` alone.
		if !t.GetActive() {
			continue
		}
		spec, err := specFromSchedule(t.GetSchedule(), now)
		if err != nil {
			continue
		}
		// A deleted task stops generating the day it was deleted. Before
		// that date it behaves exactly as it did while it existed, which is
		// what keeps its history — completed and merely due alike — intact.
		expandEnd := end
		if cutoff, ok := occurrenceCutoff(t); ok && cutoff.Before(expandEnd) {
			expandEnd = cutoff
		}
		// And it doesn't generate anything from before it existed: a chore
		// added today was not silently missed all last week. This is the
		// same reasoning occurrenceFloorDate applies to the family as a
		// whole, applied per task so that an explicit start_date can't
		// reach back past a task's own creation. It's also what makes
		// freezeOccurrences well-defined, since it has to cover exactly the
		// range this loop would otherwise derive.
		expandStart := start
		if created, err := taskCreatedDate(t); err == nil && created.After(expandStart) {
			expandStart = created
		}
		dates, err := spec.DatesBetween(expandStart, expandEnd)
		if err != nil {
			continue
		}
		for _, childID := range t.GetChildIds() {
			if childFilter != "" && childID != childFilter {
				continue
			}
			for _, d := range dates {
				dateStr := scheduling.FormatDate(d)
				key := occurrenceKey(t.GetId(), childID, dateStr)
				seen[key] = true
				if occ, ok := stored[key]; ok {
					// A stored row wins outright: its title and amount are
					// what they were when it was recorded, and re-deriving
					// them from the task as it stands now is exactly the
					// rewriting-of-history this model removes.
					occ.ChildName = childNames[childID]
					occurrences = append(occurrences, occ)
					continue
				}
				occurrences = append(occurrences, occurrenceFromTask(t, childID, childNames[childID], dateStr))
			}
		}
	}

	// A stored occurrence the loop above didn't reach — its task has since
	// been paused, or its schedule no longer covers that date — is still a
	// real, paid completion. It carries its own title and amount, so it
	// renders with no task row involved at all.
	for key, occ := range stored {
		if seen[key] {
			continue
		}
		if occ.GetDueDate() < startStr || occ.GetDueDate() > endStr {
			continue
		}
		if childFilter != "" && occ.GetChildId() != childFilter {
			continue
		}
		occ.ChildName = childNames[occ.GetChildId()]
		occurrences = append(occurrences, occ)
	}

	if search := strings.TrimSpace(req.Msg.GetSearch()); search != "" {
		ls := strings.ToLower(search)
		filtered := occurrences[:0]
		for _, o := range occurrences {
			if strings.Contains(strings.ToLower(o.GetTitle()), ls) || strings.Contains(strings.ToLower(o.GetChildName()), ls) {
				filtered = append(filtered, o)
			}
		}
		occurrences = filtered
	}

	sort.Slice(occurrences, func(i, j int) bool {
		a, b := occurrences[i], occurrences[j]
		if a.GetDueDate() != b.GetDueDate() {
			return a.GetDueDate() > b.GetDueDate()
		}
		if a.GetTitle() != b.GetTitle() {
			return a.GetTitle() < b.GetTitle()
		}
		return a.GetChildName() < b.GetChildName()
	})

	hasMore := false
	if limit := int(req.Msg.GetLimit()); limit > 0 {
		if limit > maxOccurrencesLimit {
			limit = maxOccurrencesLimit
		}
		offset := int(req.Msg.GetOffset())
		if offset < 0 {
			offset = 0
		}
		if offset > len(occurrences) {
			offset = len(occurrences)
		}
		sliceEnd := offset + limit
		if sliceEnd < len(occurrences) {
			hasMore = true
		} else {
			sliceEnd = len(occurrences)
		}
		occurrences = occurrences[offset:sliceEnd]
	}

	return connect.NewResponse(&v1.ListTaskOccurrencesResponse{Occurrences: occurrences, HasMore: hasMore}), nil
}

// freezeOccurrences records, as rows, every occurrence this task has
// already produced that nothing has been written about yet — each carrying
// the task's values as they stand *now*, before the caller changes them.
//
// This is what makes editing a task non-retroactive for occurrences nobody
// completed. A completed occurrence was already frozen when it was
// completed; an uncompleted past one exists only as a derivation from the
// task's current schedule and price, so without this an edit would silently
// restate what last month's missed chores were worth and when they were
// due.
//
// Only dates strictly before today are frozen. Today and later still track
// the task, which is what you want: correcting a price this morning should
// apply to this morning's chore, not just tomorrow's.
//
// Deliberately not called from DeleteTask. Deletion is soft and changes
// none of the values an occurrence is derived from, so a deleted task's
// past reconstructs correctly from the row that's still there — see
// occurrenceCutoff.
func (s *Server) freezeOccurrences(ctx context.Context, q querier, task *v1.Task, now time.Time) error {
	spec, err := specFromSchedule(task.GetSchedule(), now)
	if err != nil {
		return nil // an unparseable schedule produced no occurrences to freeze
	}
	start, err := taskCreatedDate(task)
	if err != nil {
		return err
	}
	end, err := scheduling.ParseDate(scheduling.FormatDate(now))
	if err != nil {
		return err
	}
	end = end.AddDate(0, 0, -1) // strictly before today
	if end.Before(start) {
		return nil
	}
	if cutoff, ok := occurrenceCutoff(task); ok && cutoff.Before(end) {
		end = cutoff
	}
	dates, err := spec.DatesBetween(start, end)
	if err != nil || len(dates) == 0 {
		return nil
	}
	iconType, iconValue, err := taskIconToDB(task.GetIcon())
	if err != nil {
		return err
	}

	// Batched: a daily task a year old freezes a few hundred rows per
	// assigned child, and this runs inside a transaction holding the only
	// connection. One statement per row turns a routine edit into a
	// visible stall.
	//
	// DO NOTHING because a row that already exists is either a completion
	// or an earlier freeze — both already say what this occurrence was,
	// and neither should be restated.
	const cols = 10
	// SQLite's default parameter ceiling is 999; this keeps a chunk well
	// under it with room for the statement itself.
	const perChunk = 90

	var args []any
	flush := func() error {
		if len(args) == 0 {
			return nil
		}
		rows := len(args) / cols
		placeholders := strings.TrimSuffix(strings.Repeat("(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL),", rows), ",")
		_, err := q.ExecContext(ctx,
			`INSERT INTO task_occurrences (id, task_id, child_id, family_id, due_date, title, description, icon_type, icon_value, amount_cents, completed_at)
			 VALUES `+placeholders+`
			 ON CONFLICT (task_id, child_id, due_date) DO NOTHING`,
			args...,
		)
		args = args[:0]
		return err
	}

	for _, childID := range task.GetChildIds() {
		for _, d := range dates {
			args = append(args,
				newID(), task.GetId(), childID, task.GetFamilyId(), scheduling.FormatDate(d),
				task.GetTitle(), task.GetDescription(), iconType, iconValue, task.GetPrice().GetCents(),
			)
			if len(args)/cols >= perChunk {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}
	return flush()
}

// restatesHistory reports whether an update would change what an
// already-due occurrence renders as, and therefore whether the task's past
// has to be frozen before it is applied.
//
// Only the fields an occurrence is derived from count. Pausing or resuming
// a task changes `active`, which affects whether it comes due from here on
// but says nothing about what it was worth or called last month — so a
// pause writes nothing. That distinction matters in practice: freezing is
// one row per past due date per assigned child, so doing it on every edit
// would turn pausing a long-running daily chore into a thousand-row write
// for no gain.
func restatesHistory(existing *v1.Task, req *v1.UpdateTaskRequest) bool {
	if existing.GetTitle() != req.GetTitle() || existing.GetDescription() != req.GetDescription() {
		return true
	}
	if existing.GetPrice().GetCents() != req.GetPrice().GetCents() {
		return true
	}
	if !proto.Equal(existing.GetIcon(), req.GetIcon()) || !proto.Equal(existing.GetSchedule(), req.GetSchedule()) {
		return true
	}
	// Assignment changes matter because an occurrence exists per assigned
	// child: dropping a child would otherwise retract every occurrence they
	// had already been asked for.
	was := append([]string(nil), existing.GetChildIds()...)
	now := dedupeStrings(req.GetChildIds())
	sort.Strings(was)
	sort.Strings(now)
	return !slices.Equal(was, now)
}

// taskCreatedDate is the calendar date a task came into existence — the
// first date it can possibly have been due on.
func taskCreatedDate(t *v1.Task) (time.Time, error) {
	return scheduling.ParseDate(scheduling.FormatDate(t.GetCreatedAt().AsTime()))
}

// occurrenceCutoff is the last date a task generates occurrences for. A
// live task has none; a deleted one stops on the date it was deleted, so
// everything it was due for while it existed still appears and nothing
// after does.
func occurrenceCutoff(t *v1.Task) (time.Time, bool) {
	if t.GetDeletedAt() == nil {
		return time.Time{}, false
	}
	cutoff, err := scheduling.ParseDate(scheduling.FormatDate(t.GetDeletedAt().AsTime()))
	if err != nil {
		return time.Time{}, false
	}
	return cutoff, true
}

// occurrenceFromTask builds the occurrence for a date nothing has been
// recorded against yet. It's derived rather than stored, so it has no id,
// and its fields track the task as it currently stands — which is correct:
// nothing has happened to this one yet, so there's nothing to preserve.
func occurrenceFromTask(t *v1.Task, childID, childName, dueDate string) *v1.TaskOccurrence {
	return &v1.TaskOccurrence{
		FamilyId:    t.GetFamilyId(),
		TaskId:      t.GetId(),
		ChildId:     childID,
		ChildName:   childName,
		DueDate:     dueDate,
		Title:       t.GetTitle(),
		Description: t.GetDescription(),
		Icon:        t.GetIcon(),
		Amount:      money(t.GetPrice().GetCents()),
	}
}

// listTasksForOccurrences loads every task in the family, deleted ones
// included, for schedule expansion. Unlike ListTasks it carries no
// authorization of its own — callers have already established membership.
func (s *Server) listTasksForOccurrences(ctx context.Context, familyID string) ([]*v1.Task, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskColumns+` FROM tasks WHERE family_id = ? ORDER BY created_at`,
		familyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*v1.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	assignments, err := s.taskAssignmentsByFamily(ctx, familyID)
	if err != nil {
		return nil, err
	}
	for _, t := range tasks {
		t.ChildIds = assignments[t.GetId()]
	}
	return tasks, nil
}

// occurrenceFloorDate is the earliest date any occurrence in the family
// could possibly exist from: no task can have been due before it was
// created, and no stored occurrence predates its own task. Used to bound
// schedule expansion when a caller leaves start_date unset (e.g. a History
// search spanning "all time").
func (s *Server) occurrenceFloorDate(ctx context.Context, familyID string) (string, error) {
	var floor sql.NullString
	if err := s.db.QueryRowContext(ctx, `
		SELECT MIN(d) FROM (
			SELECT SUBSTR(created_at, 1, 10) AS d FROM tasks WHERE family_id = ?
			UNION ALL
			SELECT due_date AS d FROM task_occurrences WHERE family_id = ?
		)`, familyID, familyID,
	).Scan(&floor); err != nil {
		return "", err
	}
	if !floor.Valid || floor.String == "" {
		return scheduling.FormatDate(nowUTC()), nil
	}
	return floor.String, nil
}

func occurrenceKey(taskID, childID, dueDate string) string {
	return taskID + "|" + childID + "|" + dueDate
}

const occurrenceColumns = `id, task_id, child_id, family_id, due_date, title, description,
	icon_type, icon_value, amount_cents, completed_at`

// listStoredOccurrences loads every recorded occurrence in the family,
// keyed the same way the schedule loop keys the ones it derives, so the two
// can be merged.
func (s *Server) listStoredOccurrences(ctx context.Context, familyID string) (map[string]*v1.TaskOccurrence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+occurrenceColumns+` FROM task_occurrences WHERE family_id = ?`,
		familyID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := map[string]*v1.TaskOccurrence{}
	for rows.Next() {
		occ, err := scanOccurrence(rows)
		if err != nil {
			return nil, err
		}
		result[occurrenceKey(occ.GetTaskId(), occ.GetChildId(), occ.GetDueDate())] = occ
	}
	return result, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

// scanOccurrence reads a stored occurrence. Its title, description, icon
// and amount come off the row itself rather than a join, which is what lets
// history survive its task being renamed, repriced or deleted. child_name
// is left for the caller to fill in from the live user row — see the
// TaskOccurrence proto for why that one is deliberately not frozen.
func scanOccurrence(row rowScanner) (*v1.TaskOccurrence, error) {
	var occ v1.TaskOccurrence
	var iconType, iconValue string
	var amountCents int64
	var completedAt sql.NullString
	if err := row.Scan(&occ.Id, &occ.TaskId, &occ.ChildId, &occ.FamilyId, &occ.DueDate,
		&occ.Title, &occ.Description, &iconType, &iconValue, &amountCents, &completedAt); err != nil {
		return nil, err
	}
	occ.Icon = taskIconFromDB(iconType, iconValue)
	occ.Amount = money(amountCents)
	if completedAt.Valid && completedAt.String != "" {
		t, err := parseTime(completedAt.String)
		if err != nil {
			return nil, err
		}
		occ.CompletedAt = timestampPB(t)
	}
	return &occ, nil
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
