-- name: GetBookingForReview :one
SELECT b.id, b.organizer_user_id, tp.user_id AS talent_user_id, bs.code AS status_code
FROM bookings b
JOIN booking_statuses bs ON bs.id = b.booking_status_id
JOIN talent_profiles tp ON tp.id = b.talent_profile_id
WHERE b.id = $1;

-- name: InsertReview :one
INSERT INTO reviews (booking_id, author_user_id, rating, body)
VALUES ($1, $2, $3, $4)
RETURNING id, created_at;

-- name: ListReviewsForBooking :many
SELECT r.id, r.rating, r.body, r.created_at, r.author_user_id, pr.display_name AS author_display_name
FROM reviews r
JOIN profiles pr ON pr.user_id = r.author_user_id
WHERE r.booking_id = $1
ORDER BY r.created_at ASC;

-- name: GetTalentReviewSummary :one
-- Aggregate rating for a talent: every review the *organizer* left about
-- one of the talent's bookings (the talent's own review of the organizer
-- doesn't count toward the talent's rating).
SELECT count(*) AS review_count, COALESCE(avg(r.rating), 0)::float8 AS average_rating
FROM reviews r
JOIN bookings b ON b.id = r.booking_id
WHERE b.talent_profile_id = $1 AND r.author_user_id = b.organizer_user_id;
