-- Talent-owned writes. Every mutation here operates on the caller's own
-- talent_profile_id, resolved server-side from the authenticated principal.

-- name: GetTalentProfileIDByUserID :one
SELECT id FROM talent_profiles WHERE user_id = $1;

-- name: GeoPlaceIDByCode :one
SELECT g.id FROM geo_places g JOIN countries c ON c.id = g.country_id
WHERE c.code = 'GH' AND g.code = $1;

-- name: UpdateTalentProfileBasics :exec
-- home_geo_place_id: pass a resolved UUID (looked up via GeoPlaceIDByCode) or
-- NULL to clear it. Never pass an unvalidated code straight into this query.
UPDATE talent_profiles
SET headline = $2, bio = $3, home_geo_place_id = $4, updated_at = now()
WHERE talent_profiles.id = $1;

-- name: ClearTalentGenres :exec
DELETE FROM talent_genres WHERE talent_profile_id = $1;
-- name: AddTalentGenre :execrows
INSERT INTO talent_genres (talent_profile_id, genre_id) SELECT $1, id FROM genres WHERE code = $2;

-- name: ClearTalentTypes :exec
DELETE FROM talent_types_map WHERE talent_profile_id = $1;
-- name: AddTalentType :execrows
INSERT INTO talent_types_map (talent_profile_id, talent_type_id) SELECT $1, id FROM talent_types WHERE code = $2;

-- name: ClearTalentLanguages :exec
DELETE FROM talent_languages WHERE talent_profile_id = $1;
-- name: AddTalentLanguage :execrows
INSERT INTO talent_languages (talent_profile_id, language_id) SELECT $1, id FROM languages WHERE code = $2;

-- name: ClearTalentServiceAreas :exec
DELETE FROM talent_service_areas WHERE talent_profile_id = $1;
-- name: AddTalentServiceArea :execrows
INSERT INTO talent_service_areas (talent_profile_id, geo_place_id)
SELECT $1, g.id FROM geo_places g JOIN countries c ON c.id = g.country_id
WHERE c.code = 'GH' AND g.code = $2;

-- name: CloseCurrentTalentRates :exec
UPDATE talent_rates SET effective_to = now() WHERE talent_profile_id = $1 AND effective_to IS NULL;

-- name: InsertTalentRate :execrows
INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id, event_type_id, effective_from)
SELECT $1, $2::numeric, cur.id, ru.id,
       (SELECT id FROM event_types WHERE code = sqlc.narg(event_type_code)::text),
       now()
FROM currencies cur, rate_units ru
WHERE cur.code = sqlc.arg(currency_code)::text AND ru.code = sqlc.arg(rate_unit_code)::text;

-- name: UpsertPayoutAccount :one
INSERT INTO payout_accounts (
    talent_profile_id, settlement_type, bank_code, bank_name,
    account_number_last4, account_name, provider_subaccount_code
) VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (talent_profile_id) DO UPDATE SET
    settlement_type = EXCLUDED.settlement_type,
    bank_code = EXCLUDED.bank_code,
    bank_name = EXCLUDED.bank_name,
    account_number_last4 = EXCLUDED.account_number_last4,
    account_name = EXCLUDED.account_name,
    provider_subaccount_code = EXCLUDED.provider_subaccount_code,
    is_active = TRUE,
    updated_at = now()
RETURNING id, updated_at;

-- name: GetPayoutAccountByTalentProfileID :one
SELECT settlement_type, bank_name, account_number_last4, account_name, is_active, updated_at
FROM payout_accounts WHERE talent_profile_id = $1 AND is_active;
