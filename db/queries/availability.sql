-- name: InsertAvailabilityBlock :one
INSERT INTO availability_blocks (talent_profile_id, starts_at, ends_at, is_available, note)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, starts_at, ends_at, is_available, note;

-- name: GetAvailabilityBlockOwner :one
SELECT talent_profile_id FROM availability_blocks WHERE id = $1;

-- name: DeleteAvailabilityBlock :execrows
DELETE FROM availability_blocks WHERE id = $1 AND talent_profile_id = $2;

-- name: ListMyAvailabilityBlocks :many
SELECT id, starts_at, ends_at, is_available, note
FROM availability_blocks
WHERE talent_profile_id = $1
  AND ends_at >= sqlc.arg(from_ts)::timestamptz
  AND starts_at <= sqlc.arg(to_ts)::timestamptz
ORDER BY starts_at;

-- name: ListTalentAvailabilityWindows :many
-- Public-safe: no `note` (a talent's own reason for a blackout, e.g. "unwell",
-- is not something organizers should see).
SELECT id, starts_at, ends_at, is_available
FROM availability_blocks
WHERE talent_profile_id = $1
  AND ends_at >= sqlc.arg(from_ts)::timestamptz
  AND starts_at <= sqlc.arg(to_ts)::timestamptz
ORDER BY starts_at;
