// Package scheduling interprets standard cron expressions as a set of
// calendar dates a task is due on (time-of-day fields are ignored — only
// day-of-month, month and day-of-week matter).
package scheduling

import (
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
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	end = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, end.Location())

	var dates []time.Time
	cursor := start.Add(-time.Second)
	for {
		next := sched.Next(cursor)
		next = time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, next.Location())
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
