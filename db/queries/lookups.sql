-- name: ListGenres :many
SELECT id, code, name, sort_order, is_active
FROM genres
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListRoles :many
SELECT id, code, name, sort_order, is_active
FROM roles
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: GetUserByEmail :one
SELECT id, email, phone, password_hash, user_status_id, created_at, updated_at
FROM users
WHERE email = $1;

-- name: ListBookingStatuses :many
SELECT id, code, name, sort_order, is_active
FROM booking_statuses
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;
