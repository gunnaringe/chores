package server

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/internal/scheduling"
)

// Conversions between the API's types and how they are stored. The
// database keeps a task's schedule as a mode plus its per-mode columns; the
// oneof is an API shape, not a storage one.

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

func money(cents int64) *v1.Money {
	return &v1.Money{Cents: cents}
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

// specFromSchedule turns the API's Schedule into the scheduling package's
// Spec. The oneof means an ill-formed combination — a one-off carrying a
// cron expression, say — can't be expressed in the first place, so the only
// validation left here is what the oneof can't encode: a missing kind, and
// the per-kind requirements Spec.Validate already knows.
//
// now is consulted for one default: a weekly schedule with no anchor_date
// is anchored to it, which is needed for "every N weeks" parity and is
// harmless at an interval of 1, where the anchor goes unused. A task loaded
// from storage always has one already.
func specFromSchedule(sched *v1.Schedule, now time.Time) (scheduling.Spec, error) {
	switch kind := sched.GetKind().(type) {
	case *v1.Schedule_Once:
		return scheduling.Spec{
			Mode:          scheduling.ModeOnce,
			StartDate:     kind.Once.GetDate(),
			IntervalWeeks: 1, // unused here; kept at its natural default rather than Go's zero value
		}, nil
	case *v1.Schedule_Weekly:
		days := make([]int, len(kind.Weekly.GetDaysOfWeek()))
		for i, d := range kind.Weekly.GetDaysOfWeek() {
			days[i] = int(d)
		}
		interval := int(kind.Weekly.GetIntervalWeeks())
		if interval < 1 {
			interval = 1
		}
		anchor := kind.Weekly.GetAnchorDate()
		if anchor == "" {
			anchor = scheduling.FormatDate(now)
		}
		return scheduling.Spec{
			Mode:          scheduling.ModeWeekly,
			DaysOfWeek:    days,
			IntervalWeeks: interval,
			StartDate:     anchor,
		}, nil
	case *v1.Schedule_Cron:
		return scheduling.Spec{
			Mode:          scheduling.ModeCron,
			Cron:          kind.Cron.GetExpression(),
			IntervalWeeks: 1,
		}, nil
	default:
		return scheduling.Spec{}, errors.New("schedule is required")
	}
}

// scheduleFromSpec is specFromSchedule's inverse, for rendering a stored
// task back out over the API.
func scheduleFromSpec(spec scheduling.Spec) *v1.Schedule {
	switch spec.Mode {
	case scheduling.ModeOnce:
		return &v1.Schedule{Kind: &v1.Schedule_Once{Once: &v1.OnceSchedule{Date: spec.StartDate}}}
	case scheduling.ModeWeekly:
		return &v1.Schedule{Kind: &v1.Schedule_Weekly{Weekly: &v1.WeeklySchedule{
			DaysOfWeek:    int32SliceFrom(spec.DaysOfWeek),
			IntervalWeeks: int32(spec.IntervalWeeks),
			AnchorDate:    spec.StartDate,
		}}}
	case scheduling.ModeCron:
		return &v1.Schedule{Kind: &v1.Schedule_Cron{Cron: &v1.CronSchedule{Expression: spec.Cron}}}
	default:
		return nil
	}
}

// specFromDB rebuilds a Spec from the task table's columns, which still
// store the schedule as a mode plus its per-mode fields. The oneof is an
// API-shape change, not a storage one.
func specFromDB(mode, cronExpr, daysOfWeek string, intervalWeeks int32, startDate string) scheduling.Spec {
	spec := scheduling.Spec{Mode: scheduling.Mode(mode), StartDate: startDate, IntervalWeeks: int(intervalWeeks)}
	if spec.IntervalWeeks < 1 {
		spec.IntervalWeeks = 1
	}
	switch spec.Mode {
	case scheduling.ModeCron:
		spec.Cron = cronExpr
	case scheduling.ModeWeekly:
		for _, d := range daysOfWeekFromDB(daysOfWeek) {
			spec.DaysOfWeek = append(spec.DaysOfWeek, int(d))
		}
	}
	return spec
}
