-- name: CheckTalentBookable :one
SELECT tp.is_searchable,
       EXISTS(SELECT 1 FROM payout_accounts pa WHERE pa.talent_profile_id = tp.id AND pa.is_active) AS has_payout
FROM talent_profiles tp
WHERE tp.id = $1;

-- name: GetCurrentRateForBooking :one
SELECT r.amount::text AS amount, cur.code AS currency_code
FROM talent_rates r
JOIN currencies cur ON cur.id = r.currency_id
JOIN rate_units ru ON ru.id = r.rate_unit_id
WHERE r.talent_profile_id = $1
  AND ru.code = $2
  AND r.effective_to IS NULL
  AND (sqlc.narg(event_type_code)::text IS NULL
       OR r.event_type_id IS NULL
       OR r.event_type_id = (SELECT id FROM event_types WHERE code = sqlc.narg(event_type_code)::text))
-- Prefer a rate specific to the requested event type over a general one.
ORDER BY (r.event_type_id IS NULL) ASC, r.amount ASC
LIMIT 1;

-- name: CountConflictingAgreedBookings :one
-- Interval overlap test: an existing booking conflicts with the requested
-- [new_starts_at, new_ends_at) window if it starts before the new window
-- ends AND ends after the new window starts.
SELECT count(*) FROM bookings
WHERE talent_profile_id = $1
  AND booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'agreed')
  AND starts_at < sqlc.arg(new_ends_at)::timestamptz
  AND ends_at > sqlc.arg(new_starts_at)::timestamptz
  AND (sqlc.narg(exclude_id)::uuid IS NULL OR id != sqlc.narg(exclude_id)::uuid);

-- name: InsertBooking :one
INSERT INTO bookings (
    organizer_user_id, talent_profile_id, event_type_id, geo_place_id,
    booking_status_id, starts_at, ends_at, venue_text, quoted_amount, currency_code
)
SELECT $1, $2,
       (SELECT id FROM event_types WHERE code = sqlc.narg(event_type_code)::text),
       (SELECT g.id FROM geo_places g JOIN countries c ON c.id = g.country_id WHERE c.code = 'GH' AND g.code = sqlc.narg(place_code)::text),
       (SELECT id FROM booking_statuses WHERE code = 'inquiry'),
       $3, $4, $5, $6, $7
RETURNING id, created_at;

-- name: InsertInitialBookingOffer :exec
INSERT INTO booking_offers (booking_id, round, offered_by_user_id, amount, currency_code, starts_at, ends_at, note)
SELECT b.id, 1, b.organizer_user_id, b.quoted_amount, COALESCE(b.currency_code, 'GHS'), b.starts_at, b.ends_at, sqlc.narg(note)::text
FROM bookings b WHERE b.id = $1;

-- name: InsertBookingStatusEvent :exec
INSERT INTO booking_status_events (booking_id, booking_status_id, actor_user_id, note)
SELECT $1, (SELECT id FROM booking_statuses WHERE code = sqlc.arg(status_code)::text), $2, $3;

-- name: GetBookingForTransition :one
SELECT b.id, b.organizer_user_id, b.talent_profile_id, tp.user_id AS talent_user_id,
       bs.code AS status_code, b.starts_at, b.ends_at,
       COALESCE(lo.offered_by_user_id, b.organizer_user_id) AS last_offer_by_user_id,
       COALESCE(lo.round, 1)::int AS last_round
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
LEFT JOIN LATERAL (
    SELECT o.offered_by_user_id, o.round FROM booking_offers o WHERE o.booking_id = b.id ORDER BY o.round DESC LIMIT 1
) lo ON true
WHERE b.id = $1;

-- name: LockBookingForCounter :one
-- Serialises concurrent counters/accepts on one booking.
SELECT b.id FROM bookings b WHERE b.id = $1 FOR UPDATE;

-- name: GetBookingPrice :one
SELECT quoted_amount::text AS amount, COALESCE(currency_code, 'GHS')::text AS currency_code FROM bookings WHERE id = $1;

-- name: GetBookingCurrency :one
SELECT COALESCE(currency_code, 'GHS')::text FROM bookings WHERE id = $1;

-- name: InsertBookingOffer :exec
INSERT INTO booking_offers (booking_id, round, offered_by_user_id, amount, currency_code, starts_at, ends_at, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ApplyBookingCounter :execrows
UPDATE bookings b
SET booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'countered'),
    quoted_amount = sqlc.arg(amount), starts_at = sqlc.arg(starts_at), ends_at = sqlc.arg(ends_at),
    updated_at = now()
FROM booking_statuses cur
WHERE b.id = sqlc.arg(id)::uuid AND b.booking_status_id = cur.id AND cur.code IN ('inquiry', 'countered');

-- name: ListBookingOffers :many
SELECT o.id, o.round, o.offered_by_user_id, (o.offered_by_user_id = b.organizer_user_id) AS by_organizer,
       pr.display_name AS offered_by_name,
       o.amount::text AS amount, (o.amount * 100)::bigint AS amount_pesewas, o.currency_code,
       o.starts_at, o.ends_at, o.note, o.created_at
FROM booking_offers o
JOIN bookings b ON b.id = o.booking_id
JOIN profiles pr ON pr.user_id = o.offered_by_user_id
WHERE o.booking_id = $1
ORDER BY o.round ASC;

-- name: UpdateBookingStatus :execrows
UPDATE bookings b
SET booking_status_id = (SELECT id FROM booking_statuses WHERE code = sqlc.arg(new_status_code)::text),
    cancellation_reason_id = (SELECT id FROM cancellation_reasons WHERE code = sqlc.narg(cancellation_reason_code)::text),
    updated_at = now()
FROM booking_statuses cur
WHERE b.id = sqlc.arg(id)::uuid AND b.booking_status_id = cur.id AND cur.code = sqlc.arg(expected_status_code)::text;

-- name: InsertContract :exec
INSERT INTO contracts (booking_id, terms_text, agreed_at) VALUES ($1, $2, now());

-- name: ListMyBookingsAsOrganizer :many
SELECT b.id, b.starts_at, b.ends_at, b.venue_text, b.quoted_amount::text AS quoted_amount, b.currency_code,
       bs.code AS status_code, bs.name AS status_name,
       pr.display_name AS talent_display_name, b.talent_profile_id,
       et.name AS event_type_name, b.created_at,
       (SELECT o.offered_by_user_id FROM booking_offers o WHERE o.booking_id = b.id ORDER BY o.round DESC LIMIT 1) AS last_offer_by_user_id,
       (SELECT count(*) FROM booking_offers o WHERE o.booking_id = b.id)::int AS offer_count
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
JOIN profiles pr ON pr.user_id = tp.user_id
LEFT JOIN event_types et ON et.id = b.event_type_id
WHERE b.organizer_user_id = $1
ORDER BY b.created_at DESC;

-- name: ListMyBookingsAsTalent :many
SELECT b.id, b.starts_at, b.ends_at, b.venue_text, b.quoted_amount::text AS quoted_amount, b.currency_code,
       bs.code AS status_code, bs.name AS status_name,
       pr.display_name AS organizer_display_name, b.organizer_user_id,
       et.name AS event_type_name, b.created_at,
       (SELECT o.offered_by_user_id FROM booking_offers o WHERE o.booking_id = b.id ORDER BY o.round DESC LIMIT 1) AS last_offer_by_user_id,
       (SELECT count(*) FROM booking_offers o WHERE o.booking_id = b.id)::int AS offer_count
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN profiles pr ON pr.user_id = b.organizer_user_id
LEFT JOIN event_types et ON et.id = b.event_type_id
WHERE b.talent_profile_id = $1
ORDER BY b.created_at DESC;

-- name: GetBookingDetail :one
SELECT b.id, b.organizer_user_id, b.talent_profile_id, tp.user_id AS talent_user_id,
       b.starts_at, b.ends_at, b.venue_text,
       b.quoted_amount::text AS quoted_amount, b.currency_code,
       bs.code AS status_code, bs.name AS status_name,
       organizer_pr.display_name AS organizer_display_name,
       talent_pr.display_name AS talent_display_name,
       et.name AS event_type_name,
       c.terms_text,
       (SELECT o.offered_by_user_id FROM booking_offers o WHERE o.booking_id = b.id ORDER BY o.round DESC LIMIT 1) AS last_offer_by_user_id,
       (SELECT count(*) FROM booking_offers o WHERE o.booking_id = b.id)::int AS offer_count
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN profiles organizer_pr ON organizer_pr.user_id = b.organizer_user_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
JOIN profiles talent_pr ON talent_pr.user_id = tp.user_id
LEFT JOIN event_types et ON et.id = b.event_type_id
LEFT JOIN contracts c ON c.booking_id = b.id
WHERE b.id = $1;
