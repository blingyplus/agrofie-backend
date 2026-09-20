-- Seed lookup data for Ghana talent marketplace scaffold.
-- Codes are immutable; names may change later via admin CMS.

INSERT INTO countries (code, name, sort_order) VALUES
    ('GH', 'Ghana', 1);

INSERT INTO roles (code, name, sort_order) VALUES
    ('talent', 'Talent', 1),
    ('organizer', 'Organizer', 2),
    ('admin', 'Admin', 3);

INSERT INTO user_statuses (code, name, sort_order) VALUES
    ('active', 'Active', 1),
    ('suspended', 'Suspended', 2),
    ('deleted', 'Deleted', 3);

INSERT INTO currencies (code, name, sort_order) VALUES
    ('GHS', 'Ghanaian Cedi', 1);

INSERT INTO rate_units (code, name, sort_order) VALUES
    ('per_event', 'Per event', 1),
    ('per_hour', 'Per hour', 2),
    ('per_set', 'Per set', 3);

INSERT INTO media_types (code, name, sort_order) VALUES
    ('avatar', 'Avatar', 1),
    ('photo', 'Photo', 2),
    ('audio', 'Audio', 3),
    ('video', 'Video', 4);

INSERT INTO verification_types (code, name, sort_order) VALUES
    ('identity', 'Identity', 1),
    ('media', 'Media portfolio', 2),
    ('business', 'Business credentials', 3);

INSERT INTO verification_statuses (code, name, sort_order) VALUES
    ('pending', 'Pending', 1),
    ('verified', 'Verified', 2),
    ('rejected', 'Rejected', 3);

INSERT INTO booking_statuses (code, name, sort_order) VALUES
    ('inquiry', 'Inquiry', 1),
    ('agreed', 'Agreed', 2),
    ('paid', 'Paid (held)', 3),
    ('completed', 'Completed', 4),
    ('cancelled', 'Cancelled', 5);

INSERT INTO ledger_entry_types (code, name, sort_order) VALUES
    ('hold', 'Hold', 1),
    ('release', 'Release', 2),
    ('refund', 'Refund', 3),
    ('commission', 'Commission', 4);

INSERT INTO ledger_statuses (code, name, sort_order) VALUES
    ('pending', 'Pending', 1),
    ('posted', 'Posted', 2),
    ('failed', 'Failed', 3);

INSERT INTO dispute_reasons (code, name, sort_order) VALUES
    ('no_show', 'No show', 1),
    ('quality', 'Performance quality', 2),
    ('payment', 'Payment dispute', 3),
    ('other', 'Other', 4);

INSERT INTO dispute_statuses (code, name, sort_order) VALUES
    ('open', 'Open', 1),
    ('under_review', 'Under review', 2),
    ('resolved', 'Resolved', 3),
    ('closed', 'Closed', 4);

INSERT INTO cancellation_reasons (code, name, sort_order) VALUES
    ('talent_cancel', 'Cancelled by talent', 1),
    ('organizer_cancel', 'Cancelled by organizer', 2),
    ('weather', 'Weather / force majeure', 3),
    ('other', 'Other', 4);

INSERT INTO languages (code, name, sort_order) VALUES
    ('en', 'English', 1),
    ('tw', 'Twi', 2),
    ('ga', 'Ga', 3),
    ('ee', 'Ewe', 4),
    ('dag', 'Dagbani', 5);

INSERT INTO talent_types (code, name, sort_order) VALUES
    ('musician', 'Musician', 1),
    ('traditional_performer', 'Traditional performer', 2),
    ('dj', 'DJ', 3),
    ('mc', 'MC / Host', 4),
    ('dancer', 'Dancer', 5),
    ('sound_engineer', 'Sound engineer', 6);

INSERT INTO genres (code, name, sort_order) VALUES
    ('highlife', 'Highlife', 1),
    ('hiplife', 'Hiplife', 2),
    ('afrobeats', 'Afrobeats', 3),
    ('gospel', 'Gospel', 4),
    ('traditional', 'Traditional', 5),
    ('jazz', 'Jazz', 6),
    ('reggae', 'Reggae', 7),
    ('hiphop', 'Hip-hop', 8);

INSERT INTO event_types (code, name, sort_order) VALUES
    ('wedding', 'Wedding', 1),
    ('funeral', 'Funeral', 2),
    ('festival', 'Festival', 3),
    ('corporate', 'Corporate', 4),
    ('church', 'Church', 5),
    ('private_party', 'Private party', 6);

-- Ghana 16 regions (geo_places place_level = region, parent_id null).
INSERT INTO geo_places (country_id, parent_id, code, name, place_level, sort_order)
SELECT c.id, NULL, v.code, v.name, 'region', v.sort_order
FROM countries c
CROSS JOIN (VALUES
    ('ashanti', 'Ashanti', 1),
    ('brong_ahafo', 'Bono', 2),
    ('bono_east', 'Bono East', 3),
    ('ahafo', 'Ahafo', 4),
    ('central', 'Central', 5),
    ('eastern', 'Eastern', 6),
    ('greater_accra', 'Greater Accra', 7),
    ('northern', 'Northern', 8),
    ('savannah', 'Savannah', 9),
    ('north_east', 'North East', 10),
    ('upper_east', 'Upper East', 11),
    ('upper_west', 'Upper West', 12),
    ('volta', 'Volta', 13),
    ('oti', 'Oti', 14),
    ('western', 'Western', 15),
    ('western_north', 'Western North', 16)
) AS v(code, name, sort_order)
WHERE c.code = 'GH';

-- Starter major cities under regions.
INSERT INTO geo_places (country_id, parent_id, code, name, place_level, sort_order)
SELECT c.id, p.id, v.city_code, v.city_name, 'city', v.sort_order
FROM countries c
CROSS JOIN (VALUES
    ('greater_accra', 'accra', 'Accra', 1),
    ('greater_accra', 'tema', 'Tema', 2),
    ('ashanti', 'kumasi', 'Kumasi', 3),
    ('northern', 'tamale', 'Tamale', 4),
    ('western', 'takoradi', 'Takoradi', 5),
    ('central', 'cape_coast', 'Cape Coast', 6),
    ('volta', 'ho', 'Ho', 7),
    ('eastern', 'koforidua', 'Koforidua', 8),
    ('upper_east', 'bolgatanga', 'Bolgatanga', 9),
    ('upper_west', 'wa', 'Wa', 10)
) AS v(region_code, city_code, city_name, sort_order)
JOIN geo_places p ON p.country_id = c.id AND p.code = v.region_code
WHERE c.code = 'GH';
