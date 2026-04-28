CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS rooms (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    capacity INT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id UUID NOT NULL UNIQUE REFERENCES rooms (id) ON DELETE CASCADE,
    days_of_week INT[] NOT NULL,
    start_time TIME NOT NULL,
    end_time TIME NOT NULL
);

CREATE TABLE IF NOT EXISTS slots (
    id UUID PRIMARY KEY,
    room_id UUID NOT NULL REFERENCES rooms (id) ON DELETE CASCADE,
    start_ts TIMESTAMPTZ NOT NULL,
    end_ts TIMESTAMPTZ NOT NULL,
    UNIQUE (room_id, start_ts)
);

CREATE INDEX IF NOT EXISTS idx_slots_room_start ON slots (room_id, start_ts);

CREATE TABLE IF NOT EXISTS bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slot_id UUID NOT NULL REFERENCES slots (id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users (id),
    status TEXT NOT NULL CHECK (status IN ('active', 'cancelled')),
    conference_link TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS bookings_one_active_per_slot
    ON bookings (slot_id)
    WHERE status = 'active';

CREATE INDEX IF NOT EXISTS idx_bookings_user ON bookings (user_id);

INSERT INTO users (id, email, role)
VALUES
    ('11111111-1111-4111-8111-111111111111', 'admin@dummy.local', 'admin'),
    ('22222222-2222-4222-8222-222222222222', 'user@dummy.local', 'user')
ON CONFLICT (id) DO NOTHING;
