package app

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDayMatchesSchedule(t *testing.T) {
	mon := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)
	if !dayMatchesSchedule(mon, []int{1}) {
		t.Fatal("monday")
	}
	sun := time.Date(2024, 6, 16, 0, 0, 0, 0, time.UTC)
	if !dayMatchesSchedule(sun, []int{7}) {
		t.Fatal("sunday")
	}
}

func TestBuildSlotsForDay(t *testing.T) {
	day := time.Date(2024, 6, 10, 0, 0, 0, 0, time.UTC)
	slots, err := buildSlotsForDay(day, []int{1}, "9:00", "10:00")
	if err != nil || len(slots) != 2 {
		t.Fatalf("%v %d", err, len(slots))
	}
}

func TestValidateScheduleTimes(t *testing.T) {
	if validateScheduleTimes("10:00", "10:00") == nil {
		t.Fatal("want err")
	}
	if validateScheduleTimes("9:00", "18:00") != nil {
		t.Fatal("want ok")
	}
}

func TestStableSlotIDSame(t *testing.T) {
	rid := uuid.MustParse("33333333-3333-4333-8333-333333333333")
	s := time.Date(2024, 1, 2, 9, 0, 0, 0, time.UTC)
	if stableSlotID(rid, s) != stableSlotID(rid, s) {
		t.Fatal("unstable")
	}
}
