-- Organizer-owned profile reads/writes. Always resolved from the authenticated
-- principal's user id server-side.

-- name: EnsureOrganizerProfile :one
INSERT INTO organizer_profiles (user_id) VALUES ($1)
ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
RETURNING id;

-- name: GetOrganizerProfile :one
SELECT op.id, pr.display_name, op.organization, op.phone,
       gp.code AS home_place_code, gp.name AS home_place_name
FROM organizer_profiles op
JOIN profiles pr ON pr.user_id = op.user_id
LEFT JOIN geo_places gp ON gp.id = op.home_geo_place_id
WHERE op.user_id = $1;

-- name: ListOrganizerEventTypes :many
SELECT et.code, et.name
FROM organizer_event_types m
JOIN event_types et ON et.id = m.event_type_id
WHERE m.organizer_profile_id = $1
ORDER BY et.sort_order, et.name;

-- name: UpdateOrganizerProfileBasics :exec
UPDATE organizer_profiles
SET organization = $2, phone = $3, home_geo_place_id = $4, updated_at = now()
WHERE id = $1;

-- name: UpdateProfileDisplayName :exec
UPDATE profiles SET display_name = $2, updated_at = now() WHERE user_id = $1;

-- name: ClearOrganizerEventTypes :exec
DELETE FROM organizer_event_types WHERE organizer_profile_id = $1;

-- name: AddOrganizerEventType :execrows
INSERT INTO organizer_event_types (organizer_profile_id, event_type_id)
SELECT $1, id FROM event_types WHERE code = $2
ON CONFLICT DO NOTHING;
