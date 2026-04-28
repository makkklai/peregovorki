package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Room struct {
	ID          uuid.UUID
	Name        string
	Description *string
	Capacity    *int
	CreatedAt   time.Time
}

type Schedule struct {
	ID         uuid.UUID
	RoomID     uuid.UUID
	DaysOfWeek []int
	StartTime  string
	EndTime    string
}

type Slot struct {
	ID     uuid.UUID
	RoomID uuid.UUID
	Start  time.Time
	End    time.Time
}

type Booking struct {
	ID             uuid.UUID
	SlotID         uuid.UUID
	UserID         uuid.UUID
	Status         string
	ConferenceLink *string
	CreatedAt      time.Time
}

type Pagination struct {
	Page     int
	PageSize int
	Total    int
}

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func int32ToInts(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

func intsToInt32(in []int) []int32 {
	out := make([]int32, len(in))
	for i, v := range in {
		out[i] = int32(v)
	}
	return out
}

func (s *Store) CreateRoom(ctx context.Context, name string, description *string, capacity *int) (Room, error) {
	var r Room
	err := s.pool.QueryRow(ctx, `
		INSERT INTO rooms (name, description, capacity)
		VALUES ($1, $2, $3)
		RETURNING id, name, description, capacity, created_at
	`, name, description, capacity).Scan(&r.ID, &r.Name, &r.Description, &r.Capacity, &r.CreatedAt)
	return r, err
}

func (s *Store) ListRooms(ctx context.Context) ([]Room, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, description, capacity, created_at FROM rooms ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Room
	for rows.Next() {
		var r Room
		if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Capacity, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) GetRoom(ctx context.Context, id uuid.UUID) (Room, error) {
	var r Room
	err := s.pool.QueryRow(ctx, `
		SELECT id, name, description, capacity, created_at FROM rooms WHERE id = $1
	`, id).Scan(&r.ID, &r.Name, &r.Description, &r.Capacity, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Room{}, pgx.ErrNoRows
	}
	return r, err
}

func (s *Store) CreateSchedule(ctx context.Context, roomID uuid.UUID, days []int, startHHMM, endHHMM string) (Schedule, error) {
	var sch Schedule
	var daysDB []int32
	err := s.pool.QueryRow(ctx, `
		INSERT INTO schedules (room_id, days_of_week, start_time, end_time)
		VALUES ($1, $2, $3::time, $4::time)
		RETURNING id, room_id, days_of_week, to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
	`, roomID, intsToInt32(days), startHHMM, endHHMM).Scan(&sch.ID, &sch.RoomID, &daysDB, &sch.StartTime, &sch.EndTime)
	sch.DaysOfWeek = int32ToInts(daysDB)
	return sch, err
}

func isPgUnique(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Store) GetScheduleByRoom(ctx context.Context, roomID uuid.UUID) (Schedule, bool, error) {
	var sch Schedule
	var daysDB []int32
	err := s.pool.QueryRow(ctx, `
		SELECT id, room_id, days_of_week,
			to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM schedules WHERE room_id = $1
	`, roomID).Scan(&sch.ID, &sch.RoomID, &daysDB, &sch.StartTime, &sch.EndTime)
	sch.DaysOfWeek = int32ToInts(daysDB)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, false, nil
	}
	return sch, true, err
}

func (s *Store) MaterializeAndListAvailableSlots(ctx context.Context, roomID uuid.UUID, dayUTC time.Time) ([]Slot, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	sch, ok, err := getScheduleTx(ctx, tx, roomID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	d := dayUTC.UTC()
	dayStart := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	dayEnd := dayStart.Add(24 * time.Hour)

	windows, err := buildSlotsForDay(dayStart, sch.DaysOfWeek, sch.StartTime, sch.EndTime)
	if err != nil {
		return nil, err
	}
	windows = withStableIDs(roomID, windows)

	for _, w := range windows {
		_, err := tx.Exec(ctx, `
			INSERT INTO slots (id, room_id, start_ts, end_ts)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (room_id, start_ts) DO NOTHING
		`, w.id, roomID, w.start, w.end)
		if err != nil {
			return nil, err
		}
	}

	rows, err := tx.Query(ctx, `
		SELECT s.id, s.room_id, s.start_ts, s.end_ts
		FROM slots s
		WHERE s.room_id = $1 AND s.start_ts >= $2 AND s.start_ts < $3
		AND NOT EXISTS (
			SELECT 1 FROM bookings b
			WHERE b.slot_id = s.id AND b.status = 'active'
		)
		ORDER BY s.start_ts
	`, roomID, dayStart, dayEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Slot
	for rows.Next() {
		var sl Slot
		if err := rows.Scan(&sl.ID, &sl.RoomID, &sl.Start, &sl.End); err != nil {
			return nil, err
		}
		out = append(out, sl)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

func getScheduleTx(ctx context.Context, tx pgx.Tx, roomID uuid.UUID) (Schedule, bool, error) {
	var sch Schedule
	var daysDB []int32
	err := tx.QueryRow(ctx, `
		SELECT id, room_id, days_of_week,
			to_char(start_time, 'HH24:MI'), to_char(end_time, 'HH24:MI')
		FROM schedules WHERE room_id = $1
	`, roomID).Scan(&sch.ID, &sch.RoomID, &daysDB, &sch.StartTime, &sch.EndTime)
	sch.DaysOfWeek = int32ToInts(daysDB)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, false, nil
	}
	return sch, true, err
}

func (s *Store) GetSlotByID(ctx context.Context, id uuid.UUID) (Slot, error) {
	var sl Slot
	err := s.pool.QueryRow(ctx, `
		SELECT id, room_id, start_ts, end_ts FROM slots WHERE id = $1
	`, id).Scan(&sl.ID, &sl.RoomID, &sl.Start, &sl.End)
	if errors.Is(err, pgx.ErrNoRows) {
		return Slot{}, pgx.ErrNoRows
	}
	return sl, err
}

func (s *Store) HasActiveBooking(ctx context.Context, slotID uuid.UUID) (bool, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM bookings WHERE slot_id = $1 AND status = 'active'
	`, slotID).Scan(&n)
	return n > 0, err
}

func (s *Store) CreateBooking(ctx context.Context, slotID, userID uuid.UUID, conference *string) (Booking, error) {
	var b Booking
	err := s.pool.QueryRow(ctx, `
		INSERT INTO bookings (slot_id, user_id, status, conference_link)
		VALUES ($1, $2, 'active', $3)
		RETURNING id, slot_id, user_id, status, conference_link, created_at
	`, slotID, userID, conference).Scan(&b.ID, &b.SlotID, &b.UserID, &b.Status, &b.ConferenceLink, &b.CreatedAt)
	return b, err
}

func (s *Store) CancelBookingIfOwner(ctx context.Context, bookingID, userID uuid.UUID) (Booking, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Booking{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var owner uuid.UUID
	err = tx.QueryRow(ctx, `SELECT user_id FROM bookings WHERE id = $1`, bookingID).Scan(&owner)
	if err != nil {
		return Booking{}, err
	}
	if owner != userID {
		return Booking{}, errForbidden
	}

	var b Booking
	err = tx.QueryRow(ctx, `
		UPDATE bookings SET status = 'cancelled'
		WHERE id = $1 AND user_id = $2
		RETURNING id, slot_id, user_id, status, conference_link, created_at
	`, bookingID, userID).Scan(&b.ID, &b.SlotID, &b.UserID, &b.Status, &b.ConferenceLink, &b.CreatedAt)
	if err != nil {
		return Booking{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Booking{}, err
	}
	return b, nil
}

var errForbidden = errors.New("forbidden")

func IsForbiddenCancel(err error) bool {
	return errors.Is(err, errForbidden)
}

func (s *Store) ListBookingsAdmin(ctx context.Context, page, pageSize int) ([]Booking, Pagination, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM bookings`).Scan(&total); err != nil {
		return nil, Pagination{}, err
	}

	offset := (page - 1) * pageSize
	rows, err := s.pool.Query(ctx, `
		SELECT id, slot_id, user_id, status, conference_link, created_at
		FROM bookings ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, pageSize, offset)
	if err != nil {
		return nil, Pagination{}, err
	}
	defer rows.Close()

	var out []Booking
	for rows.Next() {
		var b Booking
		if err := rows.Scan(&b.ID, &b.SlotID, &b.UserID, &b.Status, &b.ConferenceLink, &b.CreatedAt); err != nil {
			return nil, Pagination{}, err
		}
		out = append(out, b)
	}
	return out, Pagination{Page: page, PageSize: pageSize, Total: total}, rows.Err()
}

func (s *Store) ListMyFutureBookings(ctx context.Context, userID uuid.UUID, now time.Time) ([]Booking, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT b.id, b.slot_id, b.user_id, b.status, b.conference_link, b.created_at
		FROM bookings b
		JOIN slots s ON s.id = b.slot_id
		WHERE b.user_id = $1 AND s.start_ts >= $2
		ORDER BY s.start_ts ASC
	`, userID, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Booking
	for rows.Next() {
		var b Booking
		if err := rows.Scan(&b.ID, &b.SlotID, &b.UserID, &b.Status, &b.ConferenceLink, &b.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
