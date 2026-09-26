-- Discovery reads. These never select users.email / users.phone: contact details
-- stay hidden until a booking is agreed.

-- name: SearchTalent :many
WITH RECURSIVE place_subtree AS (
    SELECT g.id
    FROM geo_places g
    JOIN countries c ON c.id = g.country_id
    WHERE sqlc.arg(place_code)::text <> ''
      AND c.code = sqlc.arg(country_code)::text
      AND g.code = sqlc.arg(place_code)::text
    UNION ALL
    SELECT g.id
    FROM geo_places g
    JOIN place_subtree s ON g.parent_id = s.id
)
SELECT tp.id, tp.headline, tp.created_at, pr.display_name, hp.name AS home_place_name
FROM talent_profiles tp
JOIN profiles pr ON pr.user_id = tp.user_id
JOIN users u ON u.id = tp.user_id
JOIN user_statuses us ON us.id = u.user_status_id AND us.code = 'active'
LEFT JOIN geo_places hp ON hp.id = tp.home_geo_place_id
WHERE tp.is_searchable
  AND (cardinality(sqlc.arg(genre_codes)::text[]) = 0 OR EXISTS (
        SELECT 1 FROM talent_genres tg JOIN genres g ON g.id = tg.genre_id
        WHERE tg.talent_profile_id = tp.id AND g.code = ANY(sqlc.arg(genre_codes)::text[])))
  AND (cardinality(sqlc.arg(type_codes)::text[]) = 0 OR EXISTS (
        SELECT 1 FROM talent_types_map tm JOIN talent_types t ON t.id = tm.talent_type_id
        WHERE tm.talent_profile_id = tp.id AND t.code = ANY(sqlc.arg(type_codes)::text[])))
  AND (cardinality(sqlc.arg(language_codes)::text[]) = 0 OR EXISTS (
        SELECT 1 FROM talent_languages tl JOIN languages l ON l.id = tl.language_id
        WHERE tl.talent_profile_id = tp.id AND l.code = ANY(sqlc.arg(language_codes)::text[])))
  AND (sqlc.arg(place_code)::text = '' OR EXISTS (
        SELECT 1 FROM place_subtree ps
        WHERE ps.id = tp.home_geo_place_id
           OR EXISTS (SELECT 1 FROM talent_service_areas sa
                      WHERE sa.talent_profile_id = tp.id AND sa.geo_place_id = ps.id)))
  AND (sqlc.narg(cursor_id)::uuid IS NULL
       OR (tp.created_at, tp.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid))
ORDER BY tp.created_at DESC, tp.id DESC
LIMIT sqlc.arg(page_size)::int;

-- name: GetSearchableTalent :one
SELECT tp.id, tp.headline, tp.bio, tp.created_at, pr.display_name, hp.name AS home_place_name
FROM talent_profiles tp
JOIN profiles pr ON pr.user_id = tp.user_id
JOIN users u ON u.id = tp.user_id
JOIN user_statuses us ON us.id = u.user_status_id AND us.code = 'active'
LEFT JOIN geo_places hp ON hp.id = tp.home_geo_place_id
WHERE tp.id = $1 AND tp.is_searchable;

-- name: ListGenresForTalent :many
SELECT tg.talent_profile_id, g.code, g.name
FROM talent_genres tg
JOIN genres g ON g.id = tg.genre_id
WHERE tg.talent_profile_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY g.sort_order, g.name;

-- name: ListTypesForTalent :many
SELECT tm.talent_profile_id, t.code, t.name
FROM talent_types_map tm
JOIN talent_types t ON t.id = tm.talent_type_id
WHERE tm.talent_profile_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY t.sort_order, t.name;

-- name: ListLanguagesForTalent :many
SELECT tl.talent_profile_id, l.code, l.name
FROM talent_languages tl
JOIN languages l ON l.id = tl.language_id
WHERE tl.talent_profile_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY l.sort_order, l.name;

-- name: ListServiceAreasForTalent :many
SELECT sa.talent_profile_id, g.code, g.name
FROM talent_service_areas sa
JOIN geo_places g ON g.id = sa.geo_place_id
WHERE sa.talent_profile_id = ANY(sqlc.arg(ids)::uuid[])
ORDER BY g.sort_order, g.name;

-- name: ListCurrentRatesForTalent :many
SELECT r.talent_profile_id,
       r.amount::text AS amount,
       cur.code AS currency_code,
       ru.code AS rate_unit_code,
       et.code AS event_type_code
FROM talent_rates r
JOIN currencies cur ON cur.id = r.currency_id
JOIN rate_units ru ON ru.id = r.rate_unit_id
LEFT JOIN event_types et ON et.id = r.event_type_id
WHERE r.talent_profile_id = ANY(sqlc.arg(ids)::uuid[])
  AND r.effective_to IS NULL
  AND r.effective_from <= now()
ORDER BY r.amount ASC;
