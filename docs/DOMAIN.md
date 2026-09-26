# Domain

## Roles

Stored in `roles` lookup: `talent`, `organizer`, `admin`. Users may hold multiple roles via `user_roles`.

## Booking lifecycle

`booking_statuses` codes (order matters for UX):

1. `inquiry` — organizer requested
2. `agreed` — both sides accepted; quote snapshotted
3. `paid` — payment confirmed by a **verified Paystack webhook** (or Verify Transaction). Never set from client input.
4. `completed` — organizer confirmed the event happened
5. `cancelled` — with `cancellation_reasons`

Talent contact details unlock at `agreed`. Talent must have a verified payout account before they can be booked.

Append-only history in `booking_status_events`.

## Invariants

- Talent searchable only if verification allows and `is_searchable`.
- Rates are versioned (`talent_rates`); bookings copy amount into `quoted_amount` + `currency_code`.
- Taxonomy renames update lookup `name` only; FKs keep working via `id` / `code`.
- Payment records are append-only and keyed by Paystack reference; never mutate posted amounts. Agrofie never holds funds (see `PRODUCT.md`).

## Discovery filters

Always join M2M tables — never filter on free-text genre/region columns:

- `talent_genres` → `genres`
- `talent_types_map` → `talent_types`
- `talent_service_areas` → `geo_places`
- `talent_languages` → `languages`
- `home_geo_place_id` on profile for “based in”

## Disputes

Opened against a `booking_id` with `dispute_reasons` / `dispute_statuses`. Admin review surface in Expo `(admin)` routes.
