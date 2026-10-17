-- Organizer profile: what the organizer hosts and where they are based.
-- organization / bio already exist (000003); add home region + contact phone
-- and the event types hosted (lookup codes via event_types, many-to-many).
-- organizer_profiles.phone is a contact number only, NOT a login identifier
-- (users.phone is the unique login phone).
ALTER TABLE organizer_profiles
    ADD COLUMN home_geo_place_id UUID REFERENCES geo_places(id),
    ADD COLUMN phone TEXT;

CREATE TABLE organizer_event_types (
    organizer_profile_id UUID NOT NULL REFERENCES organizer_profiles(id) ON DELETE CASCADE,
    event_type_id        UUID NOT NULL REFERENCES event_types(id),
    PRIMARY KEY (organizer_profile_id, event_type_id)
);

-- Every organizer user gets a profile row (older accounts may lack one).
INSERT INTO organizer_profiles (user_id)
SELECT ur.user_id FROM user_roles ur JOIN roles r ON r.id = ur.role_id
WHERE r.code = 'organizer'
ON CONFLICT (user_id) DO NOTHING;
