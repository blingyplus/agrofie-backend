-- Where a talent's share of a booking payment settles. Agrofie never holds
-- funds (see docs/PRODUCT.md, Money model); this row only points at each
-- talent's own Paystack subaccount.
CREATE TABLE payout_accounts (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    talent_profile_id   UUID NOT NULL UNIQUE REFERENCES talent_profiles(id) ON DELETE CASCADE,
    settlement_type     TEXT NOT NULL CHECK (settlement_type IN ('bank', 'mobile_money')),
    bank_code           TEXT NOT NULL,
    bank_name            TEXT NOT NULL,
    account_number_last4 TEXT NOT NULL,
    account_name        TEXT NOT NULL,
    provider             TEXT NOT NULL DEFAULT 'paystack',
    provider_subaccount_code TEXT NOT NULL,
    is_active            BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Full account numbers are never stored: only the last 4 digits, for display.
-- The provider (Paystack) is the source of truth for the real account number.
