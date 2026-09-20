# Domain

## Roles

Stored in `roles` lookup: `talent`, `organizer`, `admin`. Users may hold multiple roles via `user_roles`.

## Booking lifecycle

`booking_statuses` codes (order matters for UX):

1. `inquiry` — organizer requested
2. `agreed` — both sides accepted; quote snapshotted
3. `paid` — funds held (`escrow_ledger` hold posted)
4. `completed` — event done; release triggered
5. `cancelled` — with `cancellation_reasons`

Append-only history in `booking_status_events`.

## Invariants

- Talent searchable only if verification allows and `is_searchable`.
- Rates are versioned (`talent_rates`); bookings copy amount into `quoted_amount` + `currency_code`.
- Taxonomy renames update lookup `name` only; FKs keep working via `id` / `code`.
- Escrow ledger is append-only; never mutate posted amounts.

## Discovery filters

Always join M2M tables — never filter on free-text genre/region columns:

- `talent_genres` → `genres`
- `talent_types_map` → `talent_types`
- `talent_service_areas` → `geo_places`
- `talent_languages` → `languages`
- `home_geo_place_id` on profile for “based in”

## Disputes

Opened against a `booking_id` with `dispute_reasons` / `dispute_statuses`. Admin review surface in Expo `(admin)` routes.
