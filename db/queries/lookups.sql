-- name: ListCountries :many
SELECT id, code, name, sort_order, is_active
FROM countries
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListGenres :many
SELECT id, code, name, sort_order, is_active
FROM genres
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListTalentTypes :many
SELECT id, code, name, sort_order, is_active
FROM talent_types
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListEventTypes :many
SELECT id, code, name, sort_order, is_active
FROM event_types
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListLanguages :many
SELECT id, code, name, sort_order, is_active
FROM languages
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListRoles :many
SELECT id, code, name, sort_order, is_active
FROM roles
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListBookingStatuses :many
SELECT id, code, name, sort_order, is_active
FROM booking_statuses
WHERE (sqlc.arg(active_only)::bool = false OR is_active = true)
ORDER BY sort_order, name;

-- name: ListGeoPlaces :many
SELECT
  g.id,
  g.code,
  g.name,
  g.place_level,
  p.code AS parent_code,
  g.is_active,
  g.sort_order
FROM geo_places g
JOIN countries c ON c.id = g.country_id
LEFT JOIN geo_places p ON p.id = g.parent_id
WHERE c.code = sqlc.arg(country_code)
  AND (
    (sqlc.narg(parent_code)::text IS NULL AND g.parent_id IS NULL)
    OR p.code = sqlc.narg(parent_code)
  )
  AND (sqlc.arg(active_only)::bool = false OR g.is_active = true)
ORDER BY g.sort_order, g.name;

-- name: UpdateGenreName :one
UPDATE genres
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetGenreActive :one
UPDATE genres
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateTalentTypeName :one
UPDATE talent_types
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetTalentTypeActive :one
UPDATE talent_types
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateEventTypeName :one
UPDATE event_types
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetEventTypeActive :one
UPDATE event_types
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateLanguageName :one
UPDATE languages
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetLanguageActive :one
UPDATE languages
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateRoleName :one
UPDATE roles
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetRoleActive :one
UPDATE roles
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateBookingStatusName :one
UPDATE booking_statuses
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetBookingStatusActive :one
UPDATE booking_statuses
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateDisputeReasonName :one
UPDATE dispute_reasons
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetDisputeReasonActive :one
UPDATE dispute_reasons
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: UpdateCancellationReasonName :one
UPDATE cancellation_reasons
SET name = sqlc.arg(name), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: SetCancellationReasonActive :one
UPDATE cancellation_reasons
SET is_active = sqlc.arg(is_active), updated_at = now()
WHERE code = sqlc.arg(code)
RETURNING id, code, name, sort_order, is_active;

-- name: GetUserByEmail :one
SELECT id, email, phone, password_hash, user_status_id, created_at, updated_at
FROM users
WHERE email = $1;
