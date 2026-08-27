package server

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/gunnaringe/chores/internal/scheduling"
)

// retentionDays is how much occurrence history is kept.
//
// The requirement is "this month plus the whole of last month". The widest
// that span ever gets is 61 days — 31 January back to 1 December — so a
// rolling 62-day window always contains it. Expressing it as a rolling
// window rather than calendar arithmetic buys two things: history ages out
// a day at a time instead of a month vanishing on the 1st, and every
// date comparison stays a plain string comparison on due_date.
const retentionDays = 62

// retentionCutoff is the oldest due_date still kept. Occurrences on or
// after it are retained; older ones are purged, with their earnings rolled
// into child_ledger first.
func retentionCutoff(now time.Time) string {
	return scheduling.FormatDate(now.AddDate(0, 0, -retentionDays))
}

// purgeExpiredOccurrences rolls the earnings of out-of-window occurrences
// into each child's ledger and deletes the rows, as one transaction.
//
// Doing both together is the whole point. A delete without the roll-up
// silently reduces what a child has earned, and since payouts are kept
// forever, any child paid out from those earnings is left with a negative
// balance — the same failure the occurrence table was built to prevent.
//
// Safe to run at any time and any frequency, including never: the earnings
// queries sum whatever rows still exist and add the ledger on top, so a
// balance is correct before, during and after a purge. Timing affects only
// how much disk is in use.
func (s *Server) purgeExpiredOccurrences(ctx context.Context, now time.Time) (deleted int64, err error) {
	cutoff := retentionCutoff(now)
	err = s.inTx(ctx, func(q querier) error {
		// Completed occurrences carry value, so their amounts move to the
		// ledger. Uncompleted ones carry none and simply go.
		if _, err := q.ExecContext(ctx, `
			INSERT INTO child_ledger (child_id, carried_earned_cents, updated_at)
			SELECT child_id, SUM(amount_cents), ?
			FROM task_occurrences
			WHERE due_date < ? AND completed_at IS NOT NULL
			GROUP BY child_id
			ON CONFLICT (child_id) DO UPDATE SET
				carried_earned_cents = carried_earned_cents + excluded.carried_earned_cents,
				updated_at = excluded.updated_at
		`, formatTime(now), cutoff); err != nil {
			return fmt.Errorf("carry earnings forward: %w", err)
		}
		res, err := q.ExecContext(ctx,
			`DELETE FROM task_occurrences WHERE due_date < ?`, cutoff)
		if err != nil {
			return fmt.Errorf("purge occurrences: %w", err)
		}
		deleted, _ = res.RowsAffected()

		// Soft-deleted tasks are kept only so their occurrences can resolve
		// a title; once those have aged out, the row has no readers left.
		//
		// deleted_at < cutoff is exactly the right test: a deleted task
		// stops generating occurrences on the day it went, and can't be
		// completed against afterwards, so its newest possible occurrence
		// is dated on or before deleted_at. If that is already outside the
		// window, the DELETE above has just removed every one of them.
		//
		// Same transaction as that DELETE, deliberately. Reversed or split,
		// a task could vanish while its occurrences remained, leaving
		// history with nothing to read a title from.
		if _, err := q.ExecContext(ctx,
			`DELETE FROM tasks WHERE deleted_at IS NOT NULL AND SUBSTR(deleted_at, 1, 10) < ?`,
			cutoff,
		); err != nil {
			return fmt.Errorf("purge deleted tasks: %w", err)
		}
		return nil
	})
	return deleted, err
}

// StartRetention purges out-of-window occurrences now and then daily,
// until ctx is cancelled.
//
// Best-effort by design. The machine sleeps when idle and wakes on demand,
// so "daily" is approximate and a purge can be skipped entirely — which is
// fine, because nothing depends on it having run. A missed purge costs
// disk, never correctness. That is the whole reason the earnings queries
// read through both the rows and the ledger rather than assuming the
// window is clean.
func (s *Server) StartRetention(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			deleted, err := s.purgeExpiredOccurrences(ctx, nowUTC())
			switch {
			case err != nil:
				log.Printf("retention: purge failed, will retry: %v", err)
			case deleted > 0:
				log.Printf("retention: purged %d occurrences older than %s", deleted, retentionCutoff(nowUTC()))
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// carriedEarnedCents is what a child earned before their occurrence rows
// were purged. Zero for a child whose history has never aged out.
func (s *Server) carriedEarnedCents(ctx context.Context, q querier, childID string) (int64, error) {
	var carried int64
	err := q.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(carried_earned_cents), 0) FROM child_ledger WHERE child_id = ?`,
		childID,
	).Scan(&carried)
	return carried, err
}
