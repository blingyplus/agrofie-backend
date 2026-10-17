-- Counter-offer negotiation. `countered` is a live status between inquiry and
-- agreed; every proposal (the initial inquiry included) is an immutable
-- booking_offers row so both sides can see the trail. bookings.quoted_amount /
-- starts_at / ends_at always mirror the LATEST offer; accepting locks them as
-- the snapshot that checkout and the contract use.
INSERT INTO booking_statuses (code, name, sort_order)
VALUES ('countered', 'Countered', 2)
ON CONFLICT (code) DO NOTHING;

UPDATE booking_statuses SET sort_order = CASE code
    WHEN 'inquiry'   THEN 1
    WHEN 'countered' THEN 2
    WHEN 'agreed'    THEN 3
    WHEN 'paid'      THEN 4
    WHEN 'completed' THEN 5
    WHEN 'cancelled' THEN 6
    ELSE sort_order END
WHERE code IN ('inquiry', 'countered', 'agreed', 'paid', 'completed', 'cancelled');

CREATE TABLE booking_offers (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id          UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    round               INT  NOT NULL CHECK (round >= 1),
    offered_by_user_id  UUID NOT NULL REFERENCES users(id),
    amount              NUMERIC(12, 2) NOT NULL CHECK (amount > 0),
    currency_code       TEXT NOT NULL,
    starts_at           TIMESTAMPTZ NOT NULL,
    ends_at             TIMESTAMPTZ NOT NULL,
    note                TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (booking_id, round),
    CONSTRAINT booking_offers_range_chk CHECK (ends_at > starts_at)
);

CREATE INDEX booking_offers_booking_idx ON booking_offers(booking_id, round);

-- Round 1 = the organizer's original inquiry, for every existing booking.
INSERT INTO booking_offers (booking_id, round, offered_by_user_id, amount, currency_code, starts_at, ends_at, created_at)
SELECT b.id, 1, b.organizer_user_id, COALESCE(b.quoted_amount, 0.01), COALESCE(b.currency_code, 'GHS'), b.starts_at, b.ends_at, b.created_at
FROM bookings b;
