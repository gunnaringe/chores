// Package scheduling interprets a task's repeat rule as a set of calendar
// dates it's due on. A rule is one of three modes: a one-off due date, a
// weekly repeat (on chosen days, every N weeks), or a raw cron expression
// for anything more exotic.
package scheduling

import (
	"errors"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

const dateLayout = "2006-01-02"

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// Validate returns an error if spec is not a valid 5-field cron expression.
func Validate(spec string) error {
	_, err := parser.Parse(spec)
	if err != nil {
		return fmt.Errorf("invalid schedule %q: %w", spec, err)
	}
	return nil
}

// IsDue reports whether the cron expression spec matches the given calendar
// date (time-of-day on date is ignored).
func IsDue(spec string, date time.Time) (bool, error) {
	sched, err := parser.Parse(spec)
	if err != nil {
		return false, fmt.Errorf("invalid schedule %q: %w", spec, err)
	}
	midnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	next := sched.Next(midnight.Add(-time.Second))
	return next.Year() == midnight.Year() && next.YearDay() == midnight.YearDay(), nil
}

// DatesBetween returns every calendar date between start and end (inclusive)
// that spec is due on.
func DatesBetween(spec string, start, end time.Time) ([]time.Time, error) {
	sched, err := parser.Parse(spec)
	if err != nil {
		return nil, fmt.Errorf("invalid schedule %q: %w", spec, err)
	}
	start = truncateToDate(start)
	end = truncateToDate(end)

	var dates []time.Time
	cursor := start.Add(-time.Second)
	for {
		next := sched.Next(cursor)
		next = truncateToDate(next)
		if next.After(end) {
			break
		}
		dates = append(dates, next)
		cursor = next
	}
	return dates, nil
}

func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(dateLayout, s, time.Local)
}

func FormatDate(t time.Time) string {
	return t.Format(dateLayout)
}

func truncateToDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// Mode selects how a Spec's fields are interpreted.
type Mode string

const (
	ModeOnce   Mode = "once"
	ModeWeekly Mode = "weekly"
	ModeCron   Mode = "cron"
)

// Spec is a task's repeat rule: exactly one of a one-off date, a weekly
// pattern, or a raw cron expression, selected by Mode.
type Spec struct {
	Mode Mode

	// ModeCron only.
	Cron string

	// ModeWeekly only. DaysOfWeek uses 0=Sunday..6=Saturday, matching
	// time.Weekday. IntervalWeeks must be >= 1 (1 means every week).
	DaysOfWeek    []int
	IntervalWeeks int

	// ModeOnce: the due date. ModeWeekly: the reference week
	// IntervalWeeks is counted from. Unused for ModeCron.
	StartDate string
}

// Validate reports whether the Spec is internally consistent (e.g. a
// non-empty cron expression for ModeCron, at least one day of week for
// ModeWeekly). It does not know anything about how the caller populated the
// Spec — see server.taskSpecFromFields for request-level defaulting.
func (s Spec) Validate() error {
	switch s.Mode {
	case ModeOnce:
		if s.StartDate == "" {
			return errors.New("start_date is required for a one-off task")
		}
		if _, err := ParseDate(s.StartDate); err != nil {
			return fmt.Errorf("invalid start_date: %w", err)
		}
		return nil
	case ModeWeekly:
		if len(s.DaysOfWeek) == 0 {
			return errors.New("days_of_week must include at least one day")
		}
		for _, d := range s.DaysOfWeek {
			if d < 0 || d > 6 {
				return fmt.Errorf("invalid day_of_week %d", d)
			}
		}
		if s.IntervalWeeks < 1 {
			return errors.New("repeat_interval_weeks must be at least 1")
		}
		if s.StartDate == "" {
			return errors.New("start_date is required for a weekly task")
		}
		if _, err := ParseDate(s.StartDate); err != nil {
			return fmt.Errorf("invalid start_date: %w", err)
		}
		return nil
	case ModeCron:
		if s.Cron == "" {
			return errors.New("schedule is required for a cron task")
		}
		return Validate(s.Cron)
	default:
		return fmt.Errorf("invalid repeat mode %q", s.Mode)
	}
}

// IsDue reports whether the Spec is due on the given calendar date
// (time-of-day is ignored).
func (s Spec) IsDue(date time.Time) (bool, error) {
	date = truncateToDate(date)
	switch s.Mode {
	case ModeOnce:
		d, err := ParseDate(s.StartDate)
		if err != nil {
			return false, fmt.Errorf("invalid start_date: %w", err)
		}
		return truncateToDate(d).Equal(date), nil
	case ModeWeekly:
		if !containsInt(s.DaysOfWeek, int(date.Weekday())) {
			return false, nil
		}
		interval := s.IntervalWeeks
		if interval < 1 {
			interval = 1
		}
		if interval == 1 {
			return true, nil
		}
		anchor, err := ParseDate(s.StartDate)
		if err != nil {
			return false, fmt.Errorf("invalid start_date: %w", err)
		}
		weeks := weeksBetween(anchor, date)
		return ((weeks % interval) + interval) % interval == 0, nil
	case ModeCron:
		return IsDue(s.Cron, date)
	default:
		return false, fmt.Errorf("invalid repeat mode %q", s.Mode)
	}
}

// DatesBetween returns every calendar date between start and end (inclusive)
// the Spec is due on.
func (s Spec) DatesBetween(start, end time.Time) ([]time.Time, error) {
	if s.Mode == ModeCron {
		return DatesBetween(s.Cron, start, end)
	}
	start = truncateToDate(start)
	end = truncateToDate(end)
	var dates []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		ok, err := s.IsDue(d)
		if err != nil {
			return nil, err
		}
		if ok {
			dates = append(dates, d)
		}
	}
	return dates, nil
}

func containsInt(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// weekStart returns the most recent Sunday at midnight, in date's own
// location.
func weekStart(t time.Time) time.Time {
	t = truncateToDate(t)
	return t.AddDate(0, 0, -int(t.Weekday()))
}

// weeksBetween returns how many whole weeks separate a and b's weeks
// (negative if b is before a). Both are first normalized to noon UTC on
// their week-start date purely so the subtraction can't be shifted by an
// hour across a DST transition in the original location — only the civil
// date difference matters here, not any wall-clock time.
func weeksBetween(a, b time.Time) int {
	wa, wb := weekStart(a), weekStart(b)
	na := time.Date(wa.Year(), wa.Month(), wa.Day(), 12, 0, 0, 0, time.UTC)
	nb := time.Date(wb.Year(), wb.Month(), wb.Day(), 12, 0, 0, 0, time.UTC)
	days := int(nb.Sub(na).Hours() / 24)
	return floorDiv(days, 7)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}
