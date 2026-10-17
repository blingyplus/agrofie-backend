ALTER TABLE verifications DROP COLUMN IF EXISTS evidence_media_id;
ALTER TABLE talent_profiles DROP COLUMN IF EXISTS cover_media_id, DROP COLUMN IF EXISTS photo_media_id;
DROP TABLE IF EXISTS media;
DROP TABLE IF EXISTS media_kinds;
