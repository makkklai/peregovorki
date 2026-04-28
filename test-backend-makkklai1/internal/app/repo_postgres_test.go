package app

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func poolOrSkip(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	p, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	if err := RunMigrations(ctx, p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestStoreRoomAndSchedule(t *testing.T) {
	ctx := context.Background()
	st := NewStore(poolOrSkip(t))
	r, err := st.CreateRoom(ctx, "t-room", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateSchedule(ctx, r.ID, []int{1}, "10:00", "11:00")
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateSchedule(ctx, r.ID, []int{2}, "10:00", "11:00")
	if err == nil || !isPgUnique(err) {
		t.Fatalf("want unique err got %v", err)
	}
}

func TestDoubleBookingConflict(t *testing.T) {
	ctx := context.Background()
	st := NewStore(poolOrSkip(t))
	r, _ := st.CreateRoom(ctx, "b-room", nil, nil)
	_, _ = st.CreateSchedule(ctx, r.ID, []int{1, 2, 3, 4, 5, 6, 7}, "06:00", "07:00")
	day := time.Date(2031, 1, 5, 0, 0, 0, 0, time.UTC)
	slots, err := st.MaterializeAndListAvailableSlots(ctx, r.ID, day)
	if err != nil || len(slots) < 1 {
		t.Fatalf("%v n=%d", err, len(slots))
	}
	uid := uuid.MustParse(DummyUserID)
	_, err = st.CreateBooking(ctx, slots[0].ID, uid, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateBooking(ctx, slots[0].ID, uid, nil)
	if err == nil || !isPgUnique(err) {
		t.Fatalf("want conflict %v", err)
	}
}
