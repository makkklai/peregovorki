package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type ctxKey int

const claimsKey ctxKey = 1

type Server struct {
	store     *Store
	jwtSecret []byte
}

func NewServer(st *Store, jwtSecret string) *Server {
	return &Server{store: st, jwtSecret: []byte(jwtSecret)}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)

	r.Get("/", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	r.Get("/_info", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) })
	r.Post("/dummyLogin", s.handleDummyLogin)

	r.Group(func(r chi.Router) {
		r.Use(s.authMiddleware)
		r.Get("/rooms/list", s.handleRoomsList)
		r.Get("/rooms/{roomId}/slots/list", s.handleSlotsList)
		r.With(requireRole("admin")).Post("/rooms/create", s.handleRoomCreate)
		r.With(requireRole("admin")).Post("/rooms/{roomId}/schedule/create", s.handleScheduleCreate)
		r.With(requireRole("admin")).Get("/bookings/list", s.handleBookingsList)
		r.With(requireRole("user")).Post("/bookings/create", s.handleBookingCreate)
		r.With(requireRole("user")).Get("/bookings/my", s.handleBookingsMy)
		r.With(requireRole("user")).Post("/bookings/{bookingId}/cancel", s.handleBookingCancel)
	})
	return r
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": msg},
	})
}

func writeInternal(w http.ResponseWriter) {
	writeErr(w, 500, "INTERNAL_ERROR", "internal server error")
}

func claimsFromCtx(ctx context.Context) (jwtClaims, bool) {
	c, ok := ctx.Value(claimsKey).(jwtClaims)
	return c, ok
}

func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimSpace(r.Header.Get("Authorization"))
		if raw == "" {
			writeErr(w, 401, "UNAUTHORIZED", "missing authorization")
			return
		}
		const p = "Bearer "
		if !strings.HasPrefix(raw, p) {
			writeErr(w, 401, "UNAUTHORIZED", "invalid authorization header")
			return
		}
		tok := strings.TrimSpace(raw[len(p):])
		if tok == "" {
			writeErr(w, 401, "UNAUTHORIZED", "invalid authorization header")
			return
		}
		c, err := readToken(s.jwtSecret, tok)
		if err != nil {
			writeErr(w, 401, "UNAUTHORIZED", "invalid token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), claimsKey, c)))
	})
}

func requireRole(role string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, ok := claimsFromCtx(r.Context())
			if !ok {
				writeErr(w, 401, "UNAUTHORIZED", "not authenticated")
				return
			}
			if c.Role != role {
				writeErr(w, 403, "FORBIDDEN", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) handleDummyLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid request body")
		return
	}
	if req.Role != "admin" && req.Role != "user" {
		writeErr(w, 400, "INVALID_REQUEST", "invalid role")
		return
	}
	uid, err := dummyUserID(req.Role)
	if err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid role")
		return
	}
	tok, err := makeToken(s.jwtSecret, uid, req.Role, 24*time.Hour)
	if err != nil {
		writeInternal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"token": tok})
}

func (s *Server) handleRoomsList(w http.ResponseWriter, r *http.Request) {
	rooms, err := s.store.ListRooms(r.Context())
	if err != nil {
		writeInternal(w)
		return
	}
	out := make([]map[string]any, 0, len(rooms))
	for _, room := range rooms {
		out = append(out, roomToJSON(room))
	}
	writeJSON(w, 200, map[string]any{"rooms": out})
}

func (s *Server) handleRoomCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Capacity    *int    `json:"capacity"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		writeErr(w, 400, "INVALID_REQUEST", "invalid request")
		return
	}
	room, err := s.store.CreateRoom(r.Context(), strings.TrimSpace(req.Name), req.Description, req.Capacity)
	if err != nil {
		writeInternal(w)
		return
	}
	writeJSON(w, 201, map[string]any{"room": roomToJSON(room)})
}

func (s *Server) handleScheduleCreate(w http.ResponseWriter, r *http.Request) {
	roomIDStr := chi.URLParam(r, "roomId")
	pathRoomID, err := uuid.Parse(roomIDStr)
	if err != nil {
		writeErr(w, 404, "ROOM_NOT_FOUND", "room not found")
		return
	}
	var req struct {
		RoomID     string `json:"roomId"`
		DaysOfWeek []int  `json:"daysOfWeek"`
		StartTime  string `json:"startTime"`
		EndTime    string `json:"endTime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid request body")
		return
	}
	bodyRoom, err := uuid.Parse(req.RoomID)
	if err != nil || bodyRoom != pathRoomID {
		writeErr(w, 400, "INVALID_REQUEST", "roomId mismatch")
		return
	}
	for _, d := range req.DaysOfWeek {
		if d < 1 || d > 7 {
			writeErr(w, 400, "INVALID_REQUEST", "invalid daysOfWeek")
			return
		}
	}
	if len(req.DaysOfWeek) == 0 {
		writeErr(w, 400, "INVALID_REQUEST", "daysOfWeek required")
		return
	}
	if _, err := s.store.GetRoom(r.Context(), pathRoomID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "ROOM_NOT_FOUND", "room not found")
			return
		}
		writeInternal(w)
		return
	}
	if err := validateScheduleTimes(req.StartTime, req.EndTime); err != nil {
		writeErr(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	sch, err := s.store.CreateSchedule(r.Context(), pathRoomID, req.DaysOfWeek, req.StartTime, req.EndTime)
	if err != nil {
		if isPgUnique(err) {
			writeErr(w, 409, "SCHEDULE_EXISTS", "schedule for this room already exists and cannot be changed")
			return
		}
		writeInternal(w)
		return
	}
	writeJSON(w, 201, map[string]any{"schedule": scheduleToJSON(sch)})
}

func (s *Server) handleSlotsList(w http.ResponseWriter, r *http.Request) {
	roomID, err := uuid.Parse(chi.URLParam(r, "roomId"))
	if err != nil {
		writeErr(w, 404, "ROOM_NOT_FOUND", "room not found")
		return
	}
	dateStr := r.URL.Query().Get("date")
	if dateStr == "" {
		writeErr(w, 400, "INVALID_REQUEST", "date is required")
		return
	}
	dayParsed, err := time.ParseInLocation("2006-01-02", dateStr, time.UTC)
	if err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid date")
		return
	}
	if _, err := s.store.GetRoom(r.Context(), roomID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "ROOM_NOT_FOUND", "room not found")
			return
		}
		writeInternal(w)
		return
	}
	slots, err := s.store.MaterializeAndListAvailableSlots(r.Context(), roomID, dayParsed)
	if err != nil {
		writeErr(w, 400, "INVALID_REQUEST", err.Error())
		return
	}
	out := make([]map[string]any, 0, len(slots))
	for _, sl := range slots {
		out = append(out, slotToJSON(sl))
	}
	writeJSON(w, 200, map[string]any{"slots": out})
}

func (s *Server) handleBookingCreate(w http.ResponseWriter, r *http.Request) {
	c, ok := claimsFromCtx(r.Context())
	if !ok {
		writeErr(w, 401, "UNAUTHORIZED", "not authenticated")
		return
	}
	var req struct {
		SlotID               string `json:"slotId"`
		CreateConferenceLink bool   `json:"createConferenceLink"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid request body")
		return
	}
	slotID, err := uuid.Parse(req.SlotID)
	if err != nil {
		writeErr(w, 400, "INVALID_REQUEST", "invalid slotId")
		return
	}
	userID, err := uuid.Parse(c.UserID)
	if err != nil {
		writeInternal(w)
		return
	}
	sl, err := s.store.GetSlotByID(r.Context(), slotID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "SLOT_NOT_FOUND", "slot not found")
			return
		}
		writeInternal(w)
		return
	}
	if sl.Start.Before(time.Now().UTC()) {
		writeErr(w, 400, "INVALID_REQUEST", "slot is in the past")
		return
	}
	busy, err := s.store.HasActiveBooking(r.Context(), slotID)
	if err != nil {
		writeInternal(w)
		return
	}
	if busy {
		writeErr(w, 409, "SLOT_ALREADY_BOOKED", "slot is already booked")
		return
	}
	var conf *string
	if req.CreateConferenceLink {
		link := "https://meet.mock.local/join/" + slotID.String()
		conf = &link
	}
	b, err := s.store.CreateBooking(r.Context(), slotID, userID, conf)
	if err != nil {
		if isPgUnique(err) {
			writeErr(w, 409, "SLOT_ALREADY_BOOKED", "slot is already booked")
			return
		}
		writeInternal(w)
		return
	}
	writeJSON(w, 201, map[string]any{"booking": bookingToJSON(b)})
}

func (s *Server) handleBookingsList(w http.ResponseWriter, r *http.Request) {
	page, ps, ok := parsePagination(r)
	if !ok {
		writeErr(w, 400, "INVALID_REQUEST", "invalid pagination")
		return
	}
	list, p, err := s.store.ListBookingsAdmin(r.Context(), page, ps)
	if err != nil {
		writeInternal(w)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, b := range list {
		out = append(out, bookingToJSON(b))
	}
	writeJSON(w, 200, map[string]any{
		"bookings": out,
		"pagination": map[string]any{
			"page": p.Page, "pageSize": p.PageSize, "total": p.Total,
		},
	})
}

func parsePagination(r *http.Request) (page int, pageSize int, ok bool) {
	q := r.URL.Query()
	page, pageSize = 1, 20
	if v := q.Get("page"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 {
			return 0, 0, false
		}
		page = p
	}
	if v := q.Get("pageSize"); v != "" {
		ps, err := strconv.Atoi(v)
		if err != nil || ps < 1 || ps > 100 {
			return 0, 0, false
		}
		pageSize = ps
	}
	return page, pageSize, true
}

func (s *Server) handleBookingsMy(w http.ResponseWriter, r *http.Request) {
	c, ok := claimsFromCtx(r.Context())
	if !ok {
		writeErr(w, 401, "UNAUTHORIZED", "not authenticated")
		return
	}
	userID, err := uuid.Parse(c.UserID)
	if err != nil {
		writeInternal(w)
		return
	}
	list, err := s.store.ListMyFutureBookings(r.Context(), userID, time.Now().UTC())
	if err != nil {
		writeInternal(w)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, b := range list {
		out = append(out, bookingToJSON(b))
	}
	writeJSON(w, 200, map[string]any{"bookings": out})
}

func (s *Server) handleBookingCancel(w http.ResponseWriter, r *http.Request) {
	c, ok := claimsFromCtx(r.Context())
	if !ok {
		writeErr(w, 401, "UNAUTHORIZED", "not authenticated")
		return
	}
	userID, err := uuid.Parse(c.UserID)
	if err != nil {
		writeInternal(w)
		return
	}
	bid, err := uuid.Parse(chi.URLParam(r, "bookingId"))
	if err != nil {
		writeErr(w, 404, "BOOKING_NOT_FOUND", "booking not found")
		return
	}
	b, err := s.store.CancelBookingIfOwner(r.Context(), bid, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "BOOKING_NOT_FOUND", "booking not found")
			return
		}
		if IsForbiddenCancel(err) {
			writeErr(w, 403, "FORBIDDEN", "cannot cancel another user's booking")
			return
		}
		writeInternal(w)
		return
	}
	writeJSON(w, 200, map[string]any{"booking": bookingToJSON(b)})
}

func roomToJSON(r Room) map[string]any {
	m := map[string]any{"id": r.ID.String(), "name": r.Name}
	if r.Description != nil {
		m["description"] = *r.Description
	} else {
		m["description"] = nil
	}
	if r.Capacity != nil {
		m["capacity"] = *r.Capacity
	} else {
		m["capacity"] = nil
	}
	if !r.CreatedAt.IsZero() {
		m["createdAt"] = r.CreatedAt.UTC().Format(time.RFC3339)
	} else {
		m["createdAt"] = nil
	}
	return m
}

func scheduleToJSON(s Schedule) map[string]any {
	return map[string]any{
		"id": s.ID.String(), "roomId": s.RoomID.String(),
		"daysOfWeek": s.DaysOfWeek, "startTime": s.StartTime, "endTime": s.EndTime,
	}
}

func slotToJSON(s Slot) map[string]any {
	return map[string]any{
		"id": s.ID.String(), "roomId": s.RoomID.String(),
		"start": s.Start.UTC().Format(time.RFC3339),
		"end":   s.End.UTC().Format(time.RFC3339),
	}
}

func bookingToJSON(b Booking) map[string]any {
	m := map[string]any{
		"id": b.ID.String(), "slotId": b.SlotID.String(), "userId": b.UserID.String(),
		"status": b.Status, "createdAt": b.CreatedAt.UTC().Format(time.RFC3339),
	}
	if b.ConferenceLink != nil {
		m["conferenceLink"] = *b.ConferenceLink
	} else {
		m["conferenceLink"] = nil
	}
	return m
}
