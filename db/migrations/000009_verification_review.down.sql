DROP INDEX IF EXISTS verifications_user_idx;
DROP INDEX IF EXISTS verifications_status_idx;
ALTER TABLE verifications DROP COLUMN IF EXISTS review_notes;
