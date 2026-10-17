-- name: InsertTalentRequest :one
INSERT INTO talent_requests (organizer_user_id, event_type_id, genre_id, geo_place_id, event_date, budget_amount, note, talent_request_status_id)
VALUES (
    sqlc.arg(organizer_user_id)::uuid,
    (SELECT id FROM event_types WHERE code = sqlc.narg(event_type_code)::text),
    (SELECT id FROM genres WHERE code = sqlc.narg(genre_code)::text),
    (SELECT g.id FROM geo_places g JOIN countries c ON c.id = g.country_id WHERE c.code = 'GH' AND g.code = sqlc.narg(place_code)::text),
    sqlc.narg(event_date)::date,
    sqlc.narg(budget_amount)::numeric,
    sqlc.arg(note)::text,
    (SELECT id FROM talent_request_statuses WHERE code = 'open')
)
RETURNING id;

-- name: ListTalentRequests :many
SELECT tr.id, tr.organizer_user_id, pr.display_name AS organizer_display_name,
       s.code AS status_code, s.name AS status_name,
       et.code AS event_type_code, et.name AS event_type_name,
       g.code AS genre_code, g.name AS genre_name,
       gp.code AS place_code, gp.name AS place_name,
       tr.event_date, COALESCE(round(tr.budget_amount * 100), 0)::bigint AS budget_pesewas, tr.currency_code, tr.note, tr.created_at
FROM talent_requests tr
JOIN talent_request_statuses s ON s.id = tr.talent_request_status_id
JOIN profiles pr ON pr.user_id = tr.organizer_user_id
LEFT JOIN event_types et ON et.id = tr.event_type_id
LEFT JOIN genres g ON g.id = tr.genre_id
LEFT JOIN geo_places gp ON gp.id = tr.geo_place_id
WHERE (sqlc.narg(organizer_user_id)::uuid IS NULL OR tr.organizer_user_id = sqlc.narg(organizer_user_id)::uuid)
  AND (sqlc.narg(status_code)::text IS NULL OR s.code = sqlc.narg(status_code)::text)
  AND (sqlc.narg(id)::uuid IS NULL OR tr.id = sqlc.narg(id)::uuid)
ORDER BY tr.created_at DESC;

-- name: SetTalentRequestStatus :execrows
UPDATE talent_requests
SET talent_request_status_id = (SELECT id FROM talent_request_statuses WHERE code = sqlc.arg(status_code)::text),
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid;
