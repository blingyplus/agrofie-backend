-- Adds admin resolution tracking to the disputes table (created empty in
-- 000004_bookings — this is the first slice that actually resolves one).
ALTER TABLE disputes
    ADD COLUMN resolution_note    TEXT,
    ADD COLUMN resolved_by_user_id UUID REFERENCES users(id),
    ADD COLUMN resolved_at        TIMESTAMPTZ;

CREATE INDEX disputes_booking_idx ON disputes(booking_id);
