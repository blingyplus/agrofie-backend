# Schema (high-level)

Rule: if a **name, type, status, or category can change** or needs **admin control**, it is a lookup table. Transactional rows store FKs only. Lookups: immutable `code`, mutable `name`, `sort_order`, `is_active`, `metadata jsonb`.

**Do not use Postgres ENUMs** for these values.

## Lookups

| Table | Notes |
| --- | --- |
| `countries` | Ghana (`GH`) first |
| `geo_places` | Tree: country → region → district/city (`parent_id`) |
| `roles`, `user_statuses` | Auth/authorization |
| `talent_types`, `genres` | Optional `parent_id` for future nesting |
| `languages`, `event_types`, `rate_units`, `currencies` | Commercial / profile |
| `media_types` | avatar, photo, audio, video |
| `verification_types`, `verification_statuses` | KYC pipeline |
| `booking_statuses` | inquiry → … → cancelled |
| `ledger_entry_types`, `ledger_statuses` | hold / release / refund / commission |
| `dispute_reasons`, `dispute_statuses`, `cancellation_reasons` | Ops |

## Identity vs profile

| Table | Purpose |
| --- | --- |
| `users` | Auth only (email/phone/hash/status) — **no display name** |
| `profiles` | `display_name`, `legal_name`, avatar |
| `user_roles` | M2M roles |
| `sessions` | Token sessions (Redis may cache later) |

## Talent / organizer

| Table | Purpose |
| --- | --- |
| `talent_profiles` | headline, bio, home geo, searchable flag |
| `organizer_profiles` | org fields |
| `talent_genres`, `talent_types_map`, `talent_languages`, `talent_service_areas` | M2M |
| `talent_rates` | Versioned pricing (`effective_from` / `effective_to`) |
| `media_assets`, `verifications`, `availability_blocks` | Split off profile row |

## Bookings & money

| Table | Purpose |
| --- | --- |
| `bookings` | FKs + venue_text + snapshotted quote |
| `booking_status_events` | Append-only status history |
| `contracts` | Terms document per booking |
| `escrow_ledger` | Append-only financial events |
| `reviews`, `disputes` | Post-event |

## Seed (migration `000005_seed`)

- Country Ghana; 16 regions; starter cities (Accra, Tema, Kumasi, Tamale, …)
- Roles, statuses, GHS, Ghana-relevant genres/types/event types/languages

Migrations live in `agrofie-backend/db/migrations/`.
