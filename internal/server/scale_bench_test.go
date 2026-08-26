//go:build scale

// Build-tagged out of ordinary runs: seeding a multi-tenant corpus takes
// minutes and writes most of a gigabyte.
//
//	go test -tags scale ./internal/server -run TestScale -v -timeout 30m
package server

// A scale harness for the question "does this hold up for thousands of
// families keeping years of history?". Seeded directly through SQL rather
// than the RPCs, so the dataset is built in seconds and the measurements
// are of the read paths, which is where the answer lives.
//
// Run with:
//
//	go test ./internal/server -run TestScale -v -timeout 30m

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/db"
)

type scaleParams struct {
	families         int
	childrenPerFam   int
	tasksPerFam      int
	daysOfHistory    int
	completionsPerDay int // per child
}

type scaleFixture struct {
	s        *Server
	ctx      context.Context
	familyID string
	childID  string
	rows     int
}

// seedScale builds a multi-tenant database and returns a handle onto one
// "middle" family, so measurements reflect a normal tenant rather than the
// first or last rows inserted.
func seedScale(t *testing.T, p scaleParams) scaleFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scale.db")
	conn, err := db.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	start := time.Now()
	tx, err := conn.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	insFam, _ := tx.Prepare(`INSERT INTO families (id, name, created_at) VALUES (?, ?, ?)`)
	insUser, _ := tx.Prepare(`INSERT INTO users (id, family_id, name, role, created_at, auth_subject, email) VALUES (?, ?, ?, ?, ?, ?, '')`)
	insTask, _ := tx.Prepare(`INSERT INTO tasks (id, family_id, title, description, price_cents, schedule, active, created_at, icon_type, icon_value, repeat_mode, days_of_week, repeat_interval_weeks, start_date)
		VALUES (?, ?, ?, '', ?, '0 0 * * *', 1, ?, '', '', 'cron', '', 1, '')`)
	insAssign, _ := tx.Prepare(`INSERT INTO task_assignments (task_id, child_id) VALUES (?, ?)`)
	insOcc, _ := tx.Prepare(`INSERT INTO task_occurrences (id, task_id, child_id, family_id, due_date, title, description, icon_type, icon_value, amount_cents, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, '', '', '', ?, ?)`)

	// Tasks are backdated so their whole history is legitimately due.
	taskCreated := time.Now().UTC().AddDate(0, 0, -p.daysOfHistory-1).Format(time.RFC3339)

	var midFamily, midChild string
	rows := 0
	for f := 0; f < p.families; f++ {
		famID := fmt.Sprintf("fam-%06d", f)
		insFam.Exec(famID, fmt.Sprintf("Family %d", f), now)

		var children []string
		for c := 0; c < p.childrenPerFam; c++ {
			id := fmt.Sprintf("kid-%06d-%d", f, c)
			insUser.Exec(id, famID, fmt.Sprintf("Kid %d", c), "child", now, nil)
			children = append(children, id)
		}
		parentID := fmt.Sprintf("par-%06d", f)
		insUser.Exec(parentID, famID, "Parent", "parent", now, "auth0|"+parentID)

		var tasks []string
		for k := 0; k < p.tasksPerFam; k++ {
			id := fmt.Sprintf("task-%06d-%d", f, k)
			insTask.Exec(id, famID, fmt.Sprintf("Chore %d", k), 100+k*25, taskCreated)
			for _, ch := range children {
				insAssign.Exec(id, ch)
			}
			tasks = append(tasks, id)
		}

		for d := 0; d < p.daysOfHistory; d++ {
			date := time.Now().UTC().AddDate(0, 0, -d).Format("2006-01-02")
			for ci, ch := range children {
				for k := 0; k < p.completionsPerDay && k < len(tasks); k++ {
					insOcc.Exec(
						fmt.Sprintf("occ-%06d-%d-%d-%d", f, ci, d, k),
						tasks[k], ch, famID, date, fmt.Sprintf("Chore %d", k),
						100+k*25, date+"T18:00:00Z",
					)
					rows++
				}
			}
		}
		if f == p.families/2 {
			midFamily, midChild = famID, children[0]
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := conn.Exec(`ANALYZE`); err != nil {
		t.Fatalf("analyze: %v", err)
	}

	fi, _ := os.Stat(path)
	t.Logf("seeded %d families, %d occurrence rows, %.0f MB, in %s",
		p.families, rows, float64(fi.Size())/(1<<20), time.Since(start).Round(time.Millisecond))

	return scaleFixture{
		s:        New(conn),
		ctx:      auth.NewContextWithIdentity(context.Background(), auth.Identity{Sub: "auth0|par-" + fmt.Sprintf("%06d", p.families/2), Name: "Parent"}),
		familyID: midFamily,
		childID:  midChild,
		rows:     rows,
	}
}

func timeCall(t *testing.T, label string, iters int, fn func() error) time.Duration {
	t.Helper()
	if err := fn(); err != nil { // warm up, and fail loudly rather than timing an error path
		t.Fatalf("%s: %v", label, err)
	}
	start := time.Now()
	for i := 0; i < iters; i++ {
		if err := fn(); err != nil {
			t.Fatalf("%s: %v", label, err)
		}
	}
	per := time.Since(start) / time.Duration(iters)
	t.Logf("  %-46s %8.2f ms", label, float64(per.Microseconds())/1000)
	return per
}

func TestScale_ReadPaths(t *testing.T) {
	for _, p := range []scaleParams{
		{families: 50, childrenPerFam: 3, tasksPerFam: 6, daysOfHistory: 365, completionsPerDay: 4},
		{families: 500, childrenPerFam: 3, tasksPerFam: 6, daysOfHistory: 365, completionsPerDay: 4},
		// One family keeping five years, to separate "grows with the corpus"
		// from "grows with this household's own history".
		{families: 20, childrenPerFam: 3, tasksPerFam: 6, daysOfHistory: 1825, completionsPerDay: 4},
	} {
		name := fmt.Sprintf("%dfamilies_%ddays", p.families, p.daysOfHistory)
		t.Run(name, func(t *testing.T) {
			f := seedScale(t, p)

			timeCall(t, "GetChildSummary (one child's balance)", 20, func() error {
				_, err := f.s.GetChildSummary(f.ctx, connect.NewRequest(&v1.GetChildSummaryRequest{ChildId: f.childID}))
				return err
			})
			timeCall(t, "ListChildSummaries (the Balance tab)", 20, func() error {
				_, err := f.s.ListChildSummaries(f.ctx, connect.NewRequest(&v1.ListChildSummariesRequest{FamilyId: f.familyID}))
				return err
			})
			today := time.Now().UTC().Format("2006-01-02")
			timeCall(t, "ListTaskOccurrences (Today, 1 day)", 20, func() error {
				_, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
					FamilyId: f.familyID, StartDate: today, EndDate: today,
				}))
				return err
			})
			timeCall(t, "ListTaskOccurrences (History, first page)", 10, func() error {
				_, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
					FamilyId: f.familyID, Limit: 20,
				}))
				return err
			})
			timeCall(t, "ListTaskOccurrences (History search, all time)", 10, func() error {
				_, err := f.s.ListTaskOccurrences(f.ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
					FamilyId: f.familyID, Search: "Chore 2", Limit: 20,
				}))
				return err
			})
		})
	}
}

// Throughput across families, which is where the single connection shows up.
func TestScale_Concurrency(t *testing.T) {
	f := seedScale(t, scaleParams{families: 500, childrenPerFam: 3, tasksPerFam: 6, daysOfHistory: 365, completionsPerDay: 4})
	today := time.Now().UTC().Format("2006-01-02")

	for _, workers := range []int{1, 8, 32} {
		const perWorker = 15
		start := time.Now()
		var wg sync.WaitGroup
		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				// Each worker hits a different family, as separate households would.
				famIdx := (w * 7) % 500
				famID := fmt.Sprintf("fam-%06d", famIdx)
				ctx := auth.NewContextWithIdentity(context.Background(),
					auth.Identity{Sub: fmt.Sprintf("auth0|par-%06d", famIdx), Name: "Parent"})
				for i := 0; i < perWorker; i++ {
					f.s.ListTaskOccurrences(ctx, connect.NewRequest(&v1.ListTaskOccurrencesRequest{
						FamilyId: famID, StartDate: today, EndDate: today,
					}))
				}
			}(w)
		}
		wg.Wait()
		total := workers * perWorker
		elapsed := time.Since(start)
		t.Logf("  %2d concurrent workers: %d requests in %s -> %.0f req/s",
			workers, total, elapsed.Round(time.Millisecond), float64(total)/elapsed.Seconds())
	}
}
