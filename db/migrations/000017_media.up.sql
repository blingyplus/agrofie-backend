-- Media uploads (profile photos, covers, verification documents, ...).
-- Files live in object storage behind internal/media.Storage (local disk now,
-- S3/R2 later); this table is the source of truth for ownership, visibility
-- and where the bytes are. media_kinds is a lookup: immutable `code`, mutable `name`.
-- (The older media_assets table from 000003 is unused and left untouched.)
CREATE TABLE media_kinds (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code                TEXT NOT NULL UNIQUE,
    name                TEXT NOT NULL,
    -- 'public' kinds are served without auth; 'private' only to owner/admin.
    default_visibility  TEXT NOT NULL CHECK (default_visibility IN ('public', 'private')),
    sort_order          INT NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO media_kinds (code, name, default_visibility, sort_order) VALUES
    ('profile_photo',         'Profile photo',         'public',  1),
    ('cover_image',           'Cover image',           'public',  2),
    ('verification_document', 'Verification document', 'private', 3),
    ('gallery_image',         'Gallery image',         'public',  4);

CREATE TABLE media (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind_id        UUID NOT NULL REFERENCES media_kinds(id),
    mime           TEXT NOT NULL,
    size_bytes     BIGINT NOT NULL CHECK (size_bytes > 0),
    width          INT,
    height         INT,
    -- Random server-generated key prefix; variant objects are "<key>/<variant>".
    storage_key    TEXT NOT NULL UNIQUE,
    sha256         TEXT NOT NULL,
    visibility     TEXT NOT NULL CHECK (visibility IN ('public', 'private')),
    status         TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'deleted')),
    -- Variant names stored besides "original", e.g. {"thumb","medium"}.
    variants       TEXT[] NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at     TIMESTAMPTZ
);
CREATE INDEX media_owner_idx ON media(owner_user_id);
CREATE INDEX media_orphan_idx ON media(created_at) WHERE status = 'active';

ALTER TABLE talent_profiles
    ADD COLUMN photo_media_id UUID REFERENCES media(id) ON DELETE SET NULL,
    ADD COLUMN cover_media_id UUID REFERENCES media(id) ON DELETE SET NULL;

-- Evidence for a verification: an uploaded document. evidence_ref (pasted link) still works.
ALTER TABLE verifications
    ADD COLUMN evidence_media_id UUID REFERENCES media(id) ON DELETE SET NULL;
