-- Kratos owns credentials/sessions. Marketplace users map via kratos_identity_id.
-- sessions table remains unused this slice (avoid dual-write).

CREATE SCHEMA IF NOT EXISTS kratos;

ALTER TABLE users
    ADD COLUMN kratos_identity_id UUID UNIQUE;

-- Scaffold has no user rows; enforce not-null for all future inserts.
ALTER TABLE users
    ALTER COLUMN kratos_identity_id SET NOT NULL;

CREATE INDEX users_kratos_identity_idx ON users(kratos_identity_id);
