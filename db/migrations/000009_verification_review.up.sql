-- Reviewer's own note, separate from the submitter's `notes`.
ALTER TABLE verifications ADD COLUMN review_notes TEXT;
CREATE INDEX verifications_status_idx ON verifications(verification_status_id);
CREATE INDEX verifications_user_idx ON verifications(user_id);
