-- Server-side state for the Kratos recovery (forgot password) flow. The client
-- only ever sees an email + 6-digit code; the gateway/auth service keeps the
-- Kratos flow id, CSRF token and flow cookies here, keyed by the lowercased
-- email, so the code can be redeemed in a later request. One row per email
-- (a new request replaces the old flow). Rows hold no secrets that outlive the
-- Kratos flow itself (it expires after 15 minutes); they are deleted on success.
-- The same row drives throttling: at most one request per 30s and five per hour
-- per email, and five wrong codes per issued code.
CREATE TABLE password_reset_requests (
    email               TEXT PRIMARY KEY,
    flow_id             TEXT NOT NULL,
    csrf_token          TEXT NOT NULL,
    cookies             JSONB NOT NULL DEFAULT '{}'::jsonb,
    attempts            INT NOT NULL DEFAULT 0,
    settings_flow_id    TEXT,
    verified_code_hash  TEXT,
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    window_started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    window_count        INT NOT NULL DEFAULT 1
);
