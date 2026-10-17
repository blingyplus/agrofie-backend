# Domain

## Roles

Stored in `roles` lookup: `talent`, `organizer`, `admin`. Users may hold multiple roles via `user_roles`.

## Booking lifecycle

`booking_statuses` codes (order matters for UX):

1. `inquiry` — organizer requested
2. `countered` — a counter-offer is on the table (see Negotiation)
3. `agreed` — both sides accepted; the latest offer is snapshotted as the contract terms
4. `paid` — payment confirmed by a **verified Paystack webhook** (or Verify Transaction). Never set from client input.
5. `completed` — either party confirmed the event happened, once its `ends_at` has passed. (Not gated on payment release — Paystack has already settled both parties directly by the time a booking reaches `paid` — so requiring only the organizer would just be a single point of failure if they forget; either side unlocks reviews for both.)
6. `cancelled` — with `cancellation_reasons`

Talent contact details unlock at `agreed`. Talent must have a verified payout account before they can be booked.

Append-only history in `booking_status_events`.

### Negotiation (counter-offers)

- Every proposal is an immutable `booking_offers` row (round 1 = the organizer's inquiry; price from the talent's published rate). `bookings.quoted_amount` / `starts_at` / `ends_at` always mirror the **latest** offer; accepting locks them as the contract snapshot that checkout uses. The Paystack split and no-escrow design are unchanged.
- **Turn-taking.** While status is `inquiry` or `countered`, only the party who did NOT make the last move may respond: **accept** (-> `agreed`), **counter** (-> `countered`), or (talent) **decline** (-> `cancelled`). The organizer may also **cancel** at any time while live. You can never counter, accept or decline your own offer.
- A counter carries a price in **integer pesewas** (1 .. 100,000,000 = GHS 1,000,000), the booking currency only (GHS), optionally new `startsAt`/`endsAt` (both or neither; must be future, inside published availability and not overlap another agreed booking) and a note (<= 500 chars).
- At most **6 counters** per booking (`MaxCounterRounds`); after that the receiver can still accept, decline or cancel.
- `agreed`, `paid`, `completed` and `cancelled` are terminal for negotiation. Transitions lock the booking row so a counter and an accept cannot race.
- GraphQL: `counterBooking`, `acceptCounterOffer` (either party; talents may keep using `acceptBooking`), `declineBooking`, `cancelBooking`; `BookingDetail.offers` is the trail, and `awaitingMyResponse` / `lastOfferByRole` / `counterCount` tell clients whose turn it is. Parties only; contact details stay hidden until `agreed`.

## Reviews

Unlocked only once a booking is `completed`. Each side may leave exactly one review of the other (`reviews` is unique on `(booking_id, author_user_id)`) — rating 1-5 + optional body. A talent's public aggregate rating counts only reviews the *organizer* left about them, never the talent's own review of the organizer.

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
