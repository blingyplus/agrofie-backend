DROP INDEX IF EXISTS disputes_booking_idx;
ALTER TABLE disputes
    DROP COLUMN IF EXISTS resolved_at,
    DROP COLUMN IF EXISTS resolved_by_user_id,
    DROP COLUMN IF EXISTS resolution_note;
