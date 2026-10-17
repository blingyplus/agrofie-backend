DROP TABLE IF EXISTS booking_offers;
UPDATE bookings SET booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'inquiry')
WHERE booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'countered');
UPDATE booking_status_events SET booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'inquiry')
WHERE booking_status_id = (SELECT id FROM booking_statuses WHERE code = 'countered');
DELETE FROM booking_statuses WHERE code = 'countered';
UPDATE booking_statuses SET sort_order = CASE code
    WHEN 'agreed' THEN 2 WHEN 'paid' THEN 3 WHEN 'completed' THEN 4 WHEN 'cancelled' THEN 5 ELSE sort_order END
WHERE code IN ('agreed', 'paid', 'completed', 'cancelled');
