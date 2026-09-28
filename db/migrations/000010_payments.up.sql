-- Replaces the dormant escrow/hold design (Agrofie never holds funds — see
-- docs/PRODUCT.md, Money model) with real payment records for the Paystack
-- split checkout. escrow_ledger and ledger_* were unused scaffolding from
-- before that decision.
DROP TABLE IF EXISTS escrow_ledger;
DROP TABLE IF EXISTS ledger_statuses;
DROP TABLE IF EXISTS ledger_entry_types;

CREATE TABLE payment_statuses (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    sort_order  INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT TRUE,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO payment_statuses (code, name, sort_order) VALUES
    ('pending', 'Pending', 1), ('succeeded', 'Succeeded', 2), ('failed', 'Failed', 3);

-- One row per checkout attempt. Amounts are integer pesewas (Paystack's
-- minor unit), never a client-supplied value — always derived server-side
-- from the booking's own snapshotted quote.
CREATE TABLE payments (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    booking_id          UUID NOT NULL REFERENCES bookings(id),
    payment_status_id   UUID NOT NULL REFERENCES payment_statuses(id),
    provider            TEXT NOT NULL DEFAULT 'paystack',
    provider_reference  TEXT NOT NULL UNIQUE,
    amount_pesewas      BIGINT NOT NULL,
    commission_pesewas  BIGINT NOT NULL,
    currency_code       TEXT NOT NULL,
    checkout_url        TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payments_booking_idx ON payments(booking_id);

-- Raw, append-only webhook log. Unique on (reference, event_type) is the
-- idempotency guard: Paystack retries webhooks, and this makes a repeat
-- delivery a no-op instead of double-processing.
CREATE TABLE payment_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_reference  TEXT NOT NULL,
    event_type          TEXT NOT NULL,
    payload             JSONB NOT NULL,
    received_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_reference, event_type)
);
