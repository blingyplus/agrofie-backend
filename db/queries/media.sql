-- name: GetMediaKind :one
SELECT id, code, name, default_visibility FROM media_kinds WHERE code = $1 AND is_active;

-- name: InsertMedia :one
INSERT INTO media (owner_user_id, kind_id, mime, size_bytes, width, height, storage_key, sha256, visibility, variants)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, created_at;

-- name: GetMedia :one
SELECT m.id, m.owner_user_id, mk.code AS kind_code, m.mime, m.size_bytes, m.width, m.height,
       m.storage_key, m.sha256, m.visibility, m.status, m.variants, m.created_at
FROM media m JOIN media_kinds mk ON mk.id = m.kind_id
WHERE m.id = $1;

-- name: MarkMediaDeleted :execrows
UPDATE media SET status = 'deleted', deleted_at = now() WHERE id = $1 AND status = 'active';

-- name: ClearTalentMediaRefs :exec
UPDATE talent_profiles
SET photo_media_id = CASE WHEN photo_media_id = $1 THEN NULL ELSE photo_media_id END,
    cover_media_id = CASE WHEN cover_media_id = $1 THEN NULL ELSE cover_media_id END
WHERE photo_media_id = $1 OR cover_media_id = $1;

-- name: MediaInUseByVerification :one
SELECT EXISTS (SELECT 1 FROM verifications WHERE evidence_media_id = $1) AS in_use;

-- name: SetTalentPhoto :execrows
UPDATE talent_profiles SET photo_media_id = $2, updated_at = now() WHERE user_id = $1;

-- name: SetTalentCover :execrows
UPDATE talent_profiles SET cover_media_id = $2, updated_at = now() WHERE user_id = $1;

-- name: ListTalentMediaIDs :many
SELECT tp.id, tp.photo_media_id, tp.cover_media_id
FROM talent_profiles tp
WHERE tp.id = ANY(sqlc.arg(ids)::uuid[]);

-- name: ListOrphanMedia :many
-- Active media nothing points at, older than the cutoff. gallery_image is
-- excluded until a gallery table exists.
SELECT m.id, m.storage_key, m.variants, m.created_at
FROM media m JOIN media_kinds mk ON mk.id = m.kind_id
WHERE m.status = 'active' AND m.created_at < sqlc.arg(cutoff)::timestamptz
  AND mk.code <> 'gallery_image'
  AND NOT EXISTS (SELECT 1 FROM talent_profiles tp WHERE tp.photo_media_id = m.id OR tp.cover_media_id = m.id)
  AND NOT EXISTS (SELECT 1 FROM verifications v WHERE v.evidence_media_id = m.id)
ORDER BY m.created_at
LIMIT 1000;
