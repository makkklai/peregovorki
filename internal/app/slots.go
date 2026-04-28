package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var slotNamespace = uuid.MustParse("6ba7b812-9dad-11d1-80b4-00c04fd430c8")

type slotWindow struct {
	id    uuid.UUID
	start time.Time
	end   time.Time
}

func dayMatchesSchedule(dateUTC time.Time, days []int) bool {
	want := dateUTC.UTC().Weekday()
	for _, d := range days {
		if d < 1 || d > 7 {
			continue
		}
		var w time.Weekday
		if d == 7 {
			w = time.Sunday
		} else {
			w = time.Weekday(d)
		}
		if w == want {
			return true
		}
	}
	return false
}

func validateScheduleTimes(startHHMM, endHHMM string) error {
	base := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	a, err := clockOnDay(base, startHHMM)
	if err != nil {
		return fmt.Errorf("invalid startTime")
	}
	b, err := clockOnDay(base, endHHMM)
	if err != nil {
		return fmt.Errorf("invalid endTime")
	}
	if !b.After(a) {
		return fmt.Errorf("endTime must be after startTime")
	}
	return nil
}

func buildSlotsForDay(dayUTC time.Time, daysOfWeek []int, startHHMM, endHHMM string) ([]slotWindow, error) {
	if len(daysOfWeek) == 0 {
		return nil, fmt.Errorf("daysOfWeek required")
	}
	for _, d := range daysOfWeek {
		if d < 1 || d > 7 {
			return nil, fmt.Errorf("invalid day %d", d)
		}
	}

	d := dayUTC.UTC()
	dayStart := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	if !dayMatchesSchedule(dayStart, daysOfWeek) {
		return nil, nil
	}

	startClock, err := clockOnDay(dayStart, startHHMM)
	if err != nil {
		return nil, err
	}
	endClock, err := clockOnDay(dayStart, endHHMM)
	if err != nil {
		return nil, err
	}
	if !endClock.After(startClock) {
		return nil, fmt.Errorf("endTime must be after startTime")
	}

	var out []slotWindow
	for t := startClock; t.Add(30*time.Minute).Compare(endClock) <= 0; t = t.Add(30 * time.Minute) {
		out = append(out, slotWindow{start: t, end: t.Add(30 * time.Minute)})
	}
	return out, nil
}

func clockOnDay(day time.Time, hhmm string) (time.Time, error) {
	h, m, err := parseHHMM(hhmm)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(day.Year(), day.Month(), day.Day(), h, m, 0, 0, time.UTC), nil
}

func parseHHMM(hhmm string) (hour, min int, err error) {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad time")
	}
	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	min, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("bad time")
	}
	return hour, min, nil
}

func stableSlotID(roomID uuid.UUID, startUTC time.Time) uuid.UUID {
	s := fmt.Sprintf("%s|%d", roomID.String(), startUTC.UTC().UnixNano())
	return uuid.NewSHA1(slotNamespace, []byte(s))
}

func withStableIDs(roomID uuid.UUID, wins []slotWindow) []slotWindow {
	out := make([]slotWindow, len(wins))
	for i := range wins {
		out[i] = slotWindow{
			id:    stableSlotID(roomID, wins[i].start),
			start: wins[i].start,
			end:   wins[i].end,
		}
	}
	return out
}
