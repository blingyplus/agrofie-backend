-- name: GetBookingForDispute :one
SELECT b.id, b.organizer_user_id, tp.user_id AS talent_user_id, bs.code AS status_code
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
WHERE b.id = $1;

-- name: CountOpenDisputesForBooking :one
SELECT count(*) FROM disputes d
JOIN dispute_statuses ds ON ds.id = d.dispute_status_id
WHERE d.booking_id = $1 AND ds.code IN ('open', 'under_review');

-- name: InsertDispute :one
INSERT INTO disputes (booking_id, opened_by_user_id, dispute_reason_id, dispute_status_id, details)
SELECT sqlc.arg(booking_id)::uuid, sqlc.arg(opened_by_user_id)::uuid,
       (SELECT id FROM dispute_reasons WHERE code = sqlc.arg(reason_code)::text),
       (SELECT id FROM dispute_statuses WHERE code = 'open'),
       sqlc.narg(details)::text
RETURNING id, created_at;

-- name: ListDisputesForBooking :many
SELECT d.id, d.booking_id, dr.code AS reason_code, dr.name AS reason_name,
       ds.code AS status_code, ds.name AS status_name,
       d.details, d.resolution_note, d.opened_by_user_id, d.created_at
FROM disputes d
JOIN dispute_reasons dr ON dr.id = d.dispute_reason_id
JOIN dispute_statuses ds ON ds.id = d.dispute_status_id
WHERE d.booking_id = $1
ORDER BY d.created_at DESC;

-- name: ListDisputesForAdmin :many
SELECT d.id, d.booking_id, dr.code AS reason_code, dr.name AS reason_name,
       ds.code AS status_code, ds.name AS status_name,
       d.details, d.resolution_note, d.opened_by_user_id, d.created_at,
       organizer_pr.display_name AS organizer_display_name,
       talent_pr.display_name AS talent_display_name,
       bs.code AS booking_status_code, b.starts_at
FROM disputes d
JOIN dispute_reasons dr ON dr.id = d.dispute_reason_id
JOIN dispute_statuses ds ON ds.id = d.dispute_status_id
JOIN bookings b ON b.id = d.booking_id
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN profiles organizer_pr ON organizer_pr.user_id = b.organizer_user_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
JOIN profiles talent_pr ON talent_pr.user_id = tp.user_id
WHERE (sqlc.narg(status_code)::text IS NULL OR ds.code = sqlc.narg(status_code)::text)
ORDER BY d.created_at DESC;

-- name: GetDisputeForResolve :one
SELECT d.id, d.booking_id, ds.code AS status_code
FROM disputes d
JOIN dispute_statuses ds ON ds.id = d.dispute_status_id
WHERE d.id = $1;

-- name: UpdateDisputeStatus :one
UPDATE disputes d
SET dispute_status_id = (SELECT id FROM dispute_statuses WHERE code = sqlc.arg(new_status_code)::text),
    resolution_note = COALESCE(sqlc.narg(resolution_note)::text, d.resolution_note),
    resolved_by_user_id = sqlc.arg(resolved_by_user_id)::uuid,
    resolved_at = now(),
    updated_at = now()
WHERE d.id = sqlc.arg(id)::uuid
RETURNING d.id;

-- name: GetDisputeByID :one
SELECT d.id, d.booking_id, dr.code AS reason_code, dr.name AS reason_name,
       ds.code AS status_code, ds.name AS status_name,
       d.details, d.resolution_note, d.opened_by_user_id, d.created_at
FROM disputes d
JOIN dispute_reasons dr ON dr.id = d.dispute_reason_id
JOIN dispute_statuses ds ON ds.id = d.dispute_status_id
WHERE d.id = $1;
