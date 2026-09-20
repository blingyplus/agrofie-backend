CREATE TABLE bookings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    organizer_user_id   UUID NOT NULL REFERENCES users(id),
    talent_profile_id   UUID NOT NULL REFERENCES talent_profiles(id),
    event_type_id       UUID REFERENCES event_types(id),
    geo_place_id        UUID REFERENCES geo_places(id),
    booking_status_id   UUID NOT NULL REFERENCES booking_statuses(id),
    starts_at           TIMESTAMPTZ NOT NULL,
    ends_at             TIMESTAMPTZ NOT NULL,
    venue_text          TEXT,
    quoted_amount       NUMERIC(12, 2),
    currency_code       TEXT,
    cancellation_reason_id UUID REFERENCES cancellation_reasons(id),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT bookings_range_chk CHECK (ends_at > starts_at)
);

CREATE INDEX bookings_organizer_idx ON bookings(organizer_user_id);
CREATE INDEX bookings_talent_idx ON bookings(talent_profile_id);
CREATE INDEX bookings_status_idx ON bookings(booking_status_id);

CREATE TABLE booking_status_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id          UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    booking_status_id   UUID NOT NULL REFERENCES booking_statuses(id),
    actor_user_id       UUID REFERENCES users(id),
    note                TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE contracts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      UUID NOT NULL UNIQUE REFERENCES bookings(id) ON DELETE CASCADE,
    terms_text      TEXT NOT NULL,
    agreed_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE escrow_ledger (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id              UUID NOT NULL REFERENCES bookings(id),
    ledger_entry_type_id    UUID NOT NULL REFERENCES ledger_entry_types(id),
    ledger_status_id        UUID NOT NULL REFERENCES ledger_statuses(id),
    amount                  NUMERIC(12, 2) NOT NULL,
    currency_code           TEXT NOT NULL,
    provider_ref            TEXT,
    metadata                JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX escrow_ledger_booking_idx ON escrow_ledger(booking_id);

CREATE TABLE reviews (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id      UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    author_user_id  UUID NOT NULL REFERENCES users(id),
    rating          SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    body            TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (booking_id, author_user_id)
);

CREATE TABLE disputes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id          UUID NOT NULL REFERENCES bookings(id),
    opened_by_user_id   UUID NOT NULL REFERENCES users(id),
    dispute_reason_id   UUID NOT NULL REFERENCES dispute_reasons(id),
    dispute_status_id   UUID NOT NULL REFERENCES dispute_statuses(id),
    details             TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
