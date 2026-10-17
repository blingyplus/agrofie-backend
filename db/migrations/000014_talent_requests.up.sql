-- "Tell us what you need": an organizer who finds no matching talent can pitch a
-- request to the Agrofie team. Admin-only for now (talent never sees these;
-- notifying talent is a later, v2 concern). Status is a lookup, not an enum.
CREATE TABLE talent_request_statuses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    sort_order  INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO talent_request_statuses (code, name, sort_order) VALUES
    ('open',    'Open',    1),
    ('matched', 'Matched', 2),
    ('closed',  'Closed',  3);

CREATE TABLE talent_requests (
    id                          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organizer_user_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type_id               UUID REFERENCES event_types(id),
    genre_id                    UUID REFERENCES genres(id),
    geo_place_id                UUID REFERENCES geo_places(id),
    event_date                  DATE,
    -- Same convention as bookings.quoted_amount: NUMERIC(12,2) GHS; the API speaks integer pesewas.
    budget_amount               NUMERIC(12, 2) CHECK (budget_amount IS NULL OR budget_amount > 0),
    currency_code               TEXT NOT NULL DEFAULT 'GHS',
    note                        TEXT NOT NULL DEFAULT '',
    talent_request_status_id    UUID NOT NULL REFERENCES talent_request_statuses(id),
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX talent_requests_organizer_idx ON talent_requests(organizer_user_id, created_at DESC);
CREATE INDEX talent_requests_status_idx ON talent_requests(talent_request_status_id, created_at DESC);
