package scheduling

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := ParseDate(s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return d
}

func TestSpec_Once(t *testing.T) {
	spec := Spec{Mode: ModeOnce, StartDate: "2026-09-15"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	due, err := spec.IsDue(mustDate(t, "2026-09-15"))
	if err != nil || !due {
		t.Fatalf("expected due on its date, got due=%v err=%v", due, err)
	}
	for _, d := range []string{"2026-09-14", "2026-09-16", "2026-09-22"} {
		due, err := spec.IsDue(mustDate(t, d))
		if err != nil || due {
			t.Fatalf("expected not due on %s, got due=%v err=%v", d, due, err)
		}
	}
}

func TestSpec_WeeklyEveryWeek(t *testing.T) {
	// 2026-08-24 is a Monday.
	spec := Spec{Mode: ModeWeekly, DaysOfWeek: []int{1, 3, 5}, IntervalWeeks: 1, StartDate: "2026-08-24"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	dates, err := spec.DatesBetween(mustDate(t, "2026-08-24"), mustDate(t, "2026-09-06"))
	if err != nil {
		t.Fatalf("DatesBetween: %v", err)
	}
	want := []string{"2026-08-24", "2026-08-26", "2026-08-28", "2026-08-31", "2026-09-02", "2026-09-04"}
	if len(dates) != len(want) {
		t.Fatalf("expected %d dates, got %d: %v", len(want), len(dates), dates)
	}
	for i, d := range dates {
		if FormatDate(d) != want[i] {
			t.Fatalf("date %d: expected %s, got %s", i, want[i], FormatDate(d))
		}
	}
}

func TestSpec_WeeklyEveryOtherWeek(t *testing.T) {
	// Anchor week starts Monday 2026-08-24. Task is due Mondays every 2 weeks.
	spec := Spec{Mode: ModeWeekly, DaysOfWeek: []int{1}, IntervalWeeks: 2, StartDate: "2026-08-24"}
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}

	due, err := spec.IsDue(mustDate(t, "2026-08-24")) // anchor week
	if err != nil || !due {
		t.Fatalf("expected due on the anchor week, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-08-31")) // one week later: skipped
	if err != nil || due {
		t.Fatalf("expected NOT due one week after the anchor, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-09-07")) // two weeks later: due again
	if err != nil || !due {
		t.Fatalf("expected due two weeks after the anchor, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-08-17")) // one week before the anchor: skipped
	if err != nil || due {
		t.Fatalf("expected NOT due one week before the anchor, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-08-10")) // two weeks before the anchor: due
	if err != nil || !due {
		t.Fatalf("expected due two weeks before the anchor, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-08-25")) // right day count, wrong day-of-week
	if err != nil || due {
		t.Fatalf("expected NOT due on a non-matching day-of-week, got due=%v err=%v", due, err)
	}
}

func TestSpec_WeeklyEveryThirdWeek(t *testing.T) {
	spec := Spec{Mode: ModeWeekly, DaysOfWeek: []int{5}, IntervalWeeks: 3, StartDate: "2026-01-05"} // Monday
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	dates, err := spec.DatesBetween(mustDate(t, "2026-01-01"), mustDate(t, "2026-03-01"))
	if err != nil {
		t.Fatalf("DatesBetween: %v", err)
	}
	// Fridays in the anchor week (Jan 5) and every 3rd week after: Jan 9,
	// Jan 30, Feb 20.
	want := []string{"2026-01-09", "2026-01-30", "2026-02-20"}
	if len(dates) != len(want) {
		t.Fatalf("expected %d dates, got %d: %v", len(want), len(dates), dates)
	}
	for i, d := range dates {
		if FormatDate(d) != want[i] {
			t.Fatalf("date %d: expected %s, got %s", i, want[i], FormatDate(d))
		}
	}
}

func TestSpec_Cron(t *testing.T) {
	spec := Spec{Mode: ModeCron, Cron: "0 0 1 * *"} // first of every month
	if err := spec.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	due, err := spec.IsDue(mustDate(t, "2026-09-01"))
	if err != nil || !due {
		t.Fatalf("expected due on the 1st, got due=%v err=%v", due, err)
	}
	due, err = spec.IsDue(mustDate(t, "2026-09-02"))
	if err != nil || due {
		t.Fatalf("expected not due on the 2nd, got due=%v err=%v", due, err)
	}
}

func TestSpec_ValidateRejectsBadInput(t *testing.T) {
	cases := []Spec{
		{Mode: ModeOnce, StartDate: ""},
		{Mode: ModeOnce, StartDate: "not-a-date"},
		{Mode: ModeWeekly, DaysOfWeek: nil, IntervalWeeks: 1, StartDate: "2026-01-01"},
		{Mode: ModeWeekly, DaysOfWeek: []int{7}, IntervalWeeks: 1, StartDate: "2026-01-01"},
		{Mode: ModeWeekly, DaysOfWeek: []int{1}, IntervalWeeks: 0, StartDate: "2026-01-01"},
		{Mode: ModeWeekly, DaysOfWeek: []int{1}, IntervalWeeks: 1, StartDate: ""},
		{Mode: ModeCron, Cron: ""},
		{Mode: ModeCron, Cron: "not a cron"},
		{Mode: "bogus"},
	}
	for i, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("case %d: expected an error, got nil for %+v", i, c)
		}
	}
}
