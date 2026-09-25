DROP INDEX IF EXISTS users_kratos_identity_idx;
ALTER TABLE users DROP COLUMN IF EXISTS kratos_identity_id;
-- Leave kratos schema; Kratos migrate owns its tables.
