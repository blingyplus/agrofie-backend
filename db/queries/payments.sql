-- name: GetBookingForCheckout :one
-- Everything InitiateCheckout needs, in one row: who is paying (organizer),
-- who is being paid (talent's subaccount), and the server-trusted amount.
SELECT b.id, b.organizer_user_id, u.email AS organizer_email,
       b.talent_profile_id, bs.code AS status_code,
       b.quoted_amount::text AS quoted_amount, b.currency_code,
       pa.provider_subaccount_code, pa.is_active AS payout_active
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN users u ON u.id = b.organizer_user_id
LEFT JOIN payout_accounts pa ON pa.talent_profile_id = b.talent_profile_id AND pa.is_active
WHERE b.id = $1;

-- name: InsertPayment :one
INSERT INTO payments (
    booking_id, payment_status_id, provider_reference,
    amount_pesewas, commission_pesewas, currency_code, checkout_url
) VALUES (
    $1, (SELECT id FROM payment_statuses WHERE code = 'pending'),
    $2, $3, $4, $5, $6
)
RETURNING id, created_at;

-- name: GetLatestPaymentForBooking :one
SELECT p.id, p.provider_reference, p.amount_pesewas, p.commission_pesewas,
       p.currency_code, p.checkout_url, ps.code AS status_code, ps.name AS status_name,
       p.created_at, p.updated_at
FROM payments p
JOIN payment_statuses ps ON ps.id = p.payment_status_id
WHERE p.booking_id = $1
ORDER BY p.created_at DESC
LIMIT 1;

-- name: GetPaymentByReference :one
SELECT p.id, p.booking_id, p.provider_reference, ps.code AS status_code
FROM payments p
JOIN payment_statuses ps ON ps.id = p.payment_status_id
WHERE p.provider_reference = $1;

-- name: UpdatePaymentStatusByReference :execrows
UPDATE payments
SET payment_status_id = (SELECT id FROM payment_statuses WHERE code = sqlc.arg(status_code)::text),
    updated_at = now()
WHERE provider_reference = sqlc.arg(provider_reference)::text;

-- name: InsertPaymentEvent :execrows
-- ON CONFLICT DO NOTHING is the webhook idempotency guard: a retried
-- delivery of the same (reference, event_type) affects zero rows, which the
-- caller treats as "already handled".
INSERT INTO payment_events (provider_reference, event_type, payload)
VALUES ($1, $2, $3)
ON CONFLICT (provider_reference, event_type) DO NOTHING;
