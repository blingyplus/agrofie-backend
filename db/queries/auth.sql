-- name: GetUserByEmail :one
SELECT id, email, phone, password_hash, user_status_id, kratos_identity_id, created_at, updated_at
FROM users
WHERE email = $1;

-- name: GetUserByPhone :one
SELECT id, email, phone, password_hash, user_status_id, kratos_identity_id, created_at, updated_at
FROM users
WHERE phone = $1;

-- name: GetUserByID :one
SELECT id, email, phone, password_hash, user_status_id, kratos_identity_id, created_at, updated_at
FROM users
WHERE id = $1;

-- name: GetUserByKratosIdentityID :one
SELECT id, email, phone, password_hash, user_status_id, kratos_identity_id, created_at, updated_at
FROM users
WHERE kratos_identity_id = $1;

-- name: GetUserStatusIDByCode :one
SELECT id FROM user_statuses WHERE code = $1;

-- name: GetRoleIDByCode :one
SELECT id FROM roles WHERE code = $1;

-- name: InsertUser :one
INSERT INTO users (email, phone, user_status_id, kratos_identity_id)
VALUES ($1, $2, $3, $4)
RETURNING id, email, phone, password_hash, user_status_id, kratos_identity_id, created_at, updated_at;

-- name: InsertProfile :one
INSERT INTO profiles (user_id, display_name)
VALUES ($1, $2)
RETURNING id, user_id, display_name, legal_name, avatar_media_id, created_at, updated_at;

-- name: GetProfileByUserID :one
SELECT id, user_id, display_name, legal_name, avatar_media_id, created_at, updated_at
FROM profiles
WHERE user_id = $1;

-- name: InsertUserRole :exec
INSERT INTO user_roles (user_id, role_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: ListRoleCodesForUser :many
SELECT r.code
FROM user_roles ur
JOIN roles r ON r.id = ur.role_id
WHERE ur.user_id = $1
ORDER BY r.sort_order, r.code;

-- name: InsertTalentProfile :one
INSERT INTO talent_profiles (user_id)
VALUES ($1)
RETURNING id, user_id, headline, bio, home_geo_place_id, is_searchable, created_at, updated_at;

-- name: InsertOrganizerProfile :one
INSERT INTO organizer_profiles (user_id)
VALUES ($1)
RETURNING id, user_id, organization, bio, created_at, updated_at;
