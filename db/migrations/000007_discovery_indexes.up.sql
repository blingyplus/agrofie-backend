-- Discovery filters join M2M tables from the lookup side, so index the reverse columns.
CREATE INDEX talent_genres_genre_idx ON talent_genres(genre_id);
CREATE INDEX talent_types_map_type_idx ON talent_types_map(talent_type_id);
CREATE INDEX talent_languages_language_idx ON talent_languages(language_id);
CREATE INDEX talent_service_areas_place_idx ON talent_service_areas(geo_place_id);

-- Keyset pagination over searchable talent, newest first.
CREATE INDEX talent_profiles_searchable_idx ON talent_profiles(created_at DESC, id DESC) WHERE is_searchable;

-- At most one current (open-ended) rate per talent, unit and event type,
-- so "current rate" is unambiguous.
CREATE UNIQUE INDEX talent_rates_current_uniq
    ON talent_rates (talent_profile_id, rate_unit_id, COALESCE(event_type_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE effective_to IS NULL;
