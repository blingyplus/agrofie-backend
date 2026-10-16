-- Editorial placement: admins flag talent as "featured" for the Discover hero.
-- featured_rank orders featured talent (lower = earlier); NULL sorts last.
ALTER TABLE talent_profiles
    ADD COLUMN featured      BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN featured_rank INT;

CREATE INDEX talent_profiles_featured_idx ON talent_profiles(featured_rank NULLS LAST, created_at DESC)
    WHERE is_searchable AND featured;

