package server

import (
	"os"
	"strconv"
	"testing"
	"time"
)

// CHORES_TEST_SHIFT_DAYS moves the server's clock that many days into the
// future for the whole test run. Many tests use fixed calendar dates, and a
// task is never due before it was created, so a test that quietly depends on
// "now" being before one of its dates starts failing the day that date
// passes (TestTaskRepeatModes did, on 2026-10-04). CI runs the suite a
// second time shifted a few years ahead to catch that while it's still
// cheap to fix.
func TestMain(m *testing.M) {
	if days, err := strconv.Atoi(os.Getenv("CHORES_TEST_SHIFT_DAYS")); err == nil && days != 0 {
		shift := time.Duration(days) * 24 * time.Hour
		real := nowUTC
		nowUTC = func() time.Time { return real().Add(shift) }
	}
	os.Exit(m.Run())
}
