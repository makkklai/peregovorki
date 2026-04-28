//go:build integration

package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"room-booking/internal/app"
)

func dsn(t *testing.T) string {
	d := os.Getenv("DATABASE_URL")
	if d == "" {
		t.Skip("set DATABASE_URL")
	}
	return d
}

func TestRoomScheduleBookingFlow(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := app.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := app.NewStore(pool)

	room, err := st.CreateRoom(ctx, "Integration Room", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.CreateSchedule(ctx, room.ID, []int{1, 2, 3, 4, 5, 6, 7}, "08:00", "09:00")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2030, 1, 7, 0, 0, 0, 0, time.UTC)
	slots, err := st.MaterializeAndListAvailableSlots(ctx, room.ID, day)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 2 {
		t.Fatalf("slots: %d", len(slots))
	}
	uid, _ := app.DummyUserIDForIntegration("user")
	b, err := st.CreateBooking(ctx, slots[0].ID, uid, nil)
	if err != nil || b.Status != "active" {
		t.Fatalf("%v %+v", err, b)
	}
}

func TestCancelBookingTwice(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := app.RunMigrations(ctx, pool); err != nil {
		t.Fatal(err)
	}
	st := app.NewStore(pool)
	room, _ := st.CreateRoom(ctx, "Cancel Room", nil, nil)
	_, _ = st.CreateSchedule(ctx, room.ID, []int{1, 2, 3, 4, 5, 6, 7}, "10:00", "11:00")
	day := time.Date(2030, 1, 6, 0, 0, 0, 0, time.UTC)
	slots, _ := st.MaterializeAndListAvailableSlots(ctx, room.ID, day)
	uid, _ := app.DummyUserIDForIntegration("user")
	b, _ := st.CreateBooking(ctx, slots[0].ID, uid, nil)
	b2, err := st.CancelBookingIfOwner(ctx, b.ID, uid)
	if err != nil || b2.Status != "cancelled" {
		t.Fatal(err, b2)
	}
	b3, err := st.CancelBookingIfOwner(ctx, b.ID, uid)
	if err != nil || b3.Status != "cancelled" {
		t.Fatal("idempotent", err, b3)
	}
}

func TestCancelNotOwner(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	_ = app.RunMigrations(ctx, pool)
	st := app.NewStore(pool)
	room, _ := st.CreateRoom(ctx, "X", nil, nil)
	_, _ = st.CreateSchedule(ctx, room.ID, []int{1, 2, 3, 4, 5, 6, 7}, "12:00", "13:00")
	day := time.Date(2030, 1, 6, 0, 0, 0, 0, time.UTC)
	slots, _ := st.MaterializeAndListAvailableSlots(ctx, room.ID, day)
	uid, _ := app.DummyUserIDForIntegration("user")
	b, _ := st.CreateBooking(ctx, slots[0].ID, uid, nil)
	adminID := uuid.MustParse(app.DummyAdminID)
	_, err = st.CancelBookingIfOwner(ctx, b.ID, adminID)
	if err == nil || !app.IsForbiddenCancel(err) {
		t.Fatalf("want forbidden %v", err)
	}
}
