CREATE TABLE talent_profiles (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    headline            TEXT,
    bio                 TEXT,
    home_geo_place_id   UUID REFERENCES geo_places(id),
    is_searchable       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE organizer_profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    organization    TEXT,
    bio             TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE talent_types_map (
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    talent_type_id    UUID NOT NULL REFERENCES talent_types(id),
    PRIMARY KEY (talent_profile_id, talent_type_id)
);

CREATE TABLE talent_genres (
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    genre_id          UUID NOT NULL REFERENCES genres(id),
    PRIMARY KEY (talent_profile_id, genre_id)
);

CREATE TABLE talent_languages (
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    language_id       UUID NOT NULL REFERENCES languages(id),
    PRIMARY KEY (talent_profile_id, language_id)
);

CREATE TABLE talent_service_areas (
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    geo_place_id      UUID NOT NULL REFERENCES geo_places(id),
    PRIMARY KEY (talent_profile_id, geo_place_id)
);

CREATE TABLE talent_rates (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    amount            NUMERIC(12, 2) NOT NULL,
    currency_id       UUID NOT NULL REFERENCES currencies(id),
    rate_unit_id      UUID NOT NULL REFERENCES rate_units(id),
    event_type_id     UUID REFERENCES event_types(id),
    effective_from    TIMESTAMPTZ NOT NULL DEFAULT now(),
    effective_to      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX talent_rates_profile_idx ON talent_rates(talent_profile_id);

CREATE TABLE media_assets (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    talent_profile_id UUID REFERENCES talent_profiles(id) ON DELETE SET NULL,
    media_type_id     UUID NOT NULL REFERENCES media_types(id),
    url               TEXT NOT NULL,
    title             TEXT,
    sort_order        INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Deferred FK from profiles.avatar_media_id (created before media_assets).
ALTER TABLE profiles
    ADD CONSTRAINT profiles_avatar_media_fk
    FOREIGN KEY (avatar_media_id) REFERENCES media_assets(id) ON DELETE SET NULL;

CREATE TABLE verifications (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    verification_type_id    UUID NOT NULL REFERENCES verification_types(id),
    verification_status_id  UUID NOT NULL REFERENCES verification_statuses(id),
    evidence_ref            TEXT,
    notes                   TEXT,
    reviewed_by             UUID REFERENCES users(id),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE availability_blocks (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    talent_profile_id UUID NOT NULL REFERENCES talent_profiles(id) ON DELETE CASCADE,
    starts_at         TIMESTAMPTZ NOT NULL,
    ends_at           TIMESTAMPTZ NOT NULL,
    is_available      BOOLEAN NOT NULL DEFAULT FALSE,
    note              TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT availability_range_chk CHECK (ends_at > starts_at)
);

CREATE INDEX availability_blocks_profile_idx ON availability_blocks(talent_profile_id);
