# Product — Agrofie (Agorofie)

Strategic framework for a **Ghanaian talent marketplace**.

Source spec: [Agorofie High-Level Technical Specification](https://docs.google.com/document/d/1wSHOyVUSD6VyVjcUyLbr7lxt0r1nbOZMHNBrvPtyXtA/edit)  
Spelling: product/docs often say **Agorofie**; repos/brand use **Agrofie**. Treat them as the same product.

## Problem

Independent musicians, traditional performers, and event professionals struggle with discovery and payment trust. Organizers struggle to find verified talent with clear rates and availability.

## Objectives

1. **Centralized discovery** — verified talent directory with filters (region tree, genre, type, language).
2. **Standardized booking** — inquiry → agreement → contract → hold → complete → review.
3. **Financial integrity** — platform holds funds until completion (application-level escrow ledger), then releases to talent.

## Personas

| Role | Needs |
| --- | --- |
| Talent | Profile, portfolio, rates, calendar, bookings, payouts |
| Organizer | Search, request booking, pay into hold, confirm completion, review |
| Admin | Identity/media verification queue, dispute mediation (Russel Boakye) |

## Core workflows

1. **Discovery** — browse by geo tree, genre, talent type, rating.
2. **Onboarding** — talent verification; organizer credentials.
3. **Booking request** — calendar conflict check.
4. **Contract & payment** — terms agreed; funds charged to platform merchant and recorded in `escrow_ledger`.
5. **Completion** — organizer confirms; funds released; dual review.

## Business model

- Commission on successful bookings.
- Premium placement / analytics subscriptions (later).

## Governance

- Mandatory media + identity verification for searchable talent.
- Real-time availability; high cancellation rates reduce search visibility.
- Dispute module with evidence (chat, contracts).

## Out of scope for launch

Phase two — organizers publishing events, talent claiming or pitching for them, multi-slot fill — is a separate product document: [PRODUCT_V2.md](./PRODUCT_V2.md) ([Google Doc](https://docs.google.com/document/d/199r-0Q4iiPrLDTUf4I4Zz5tWLzvt6fN4anNEGpq6xQo/edit)). Launch is organizer-to-talent discovery and booking only. v2 reuses that contract, hold, and review path; it is not a second marketplace.

## Scaffold vs later

| Now (scaffold) | Later (MVP+) | After launch (v2) |
| --- | --- | --- |
| Schema, seeds, health, role shells | Auth, booking FSM, Paystack MoMo | Event listings, talent-side discovery |
| Lookup CMS stubs | Full admin CMS | Claim vs request/pitch, multi-slot events |
| Fake payment provider | Live Paystack charge + transfer | Same hold/release on event-originated bookings |
| Compose locally | DigitalOcean Kubernetes (DOKS) | Two-way entertainment marketplace |
