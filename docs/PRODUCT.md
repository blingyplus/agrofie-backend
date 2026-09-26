# Product — Agrofie (Agorofie)

Strategic framework for a **Ghanaian talent marketplace**.

Source spec: [Agorofie High-Level Technical Specification](https://docs.google.com/document/d/1wSHOyVUSD6VyVjcUyLbr7lxt0r1nbOZMHNBrvPtyXtA/edit)  
Spelling: product/docs often say **Agorofie**; repos/brand use **Agrofie**. Treat them as the same product.

## Problem

Independent musicians, traditional performers, and event professionals struggle with discovery and payment trust. Organizers struggle to find verified talent with clear rates and availability.

## Objectives

1. **Centralized discovery** — verified talent directory with filters (region tree, genre, type, language).
2. **Standardized booking** — inquiry → agreed (quote + contract snapshot) → paid → completed → review.
3. **Fast, trustworthy payment** — the organizer pays on-platform through Paystack. The talent's share settles straight to their own payout account and Agrofie's commission is taken automatically. Agrofie never holds client funds (see [Money model](#money-model)).

## Personas

| Role | Needs |
| --- | --- |
| Talent | Profile, portfolio, rates, calendar, bookings, payout account (bank or mobile money), reviews |
| Organizer | Search, request booking, agree terms, pay through Paystack (card / mobile money / bank transfer), confirm completion, review |
| Admin | Identity/media verification queue, dispute mediation (Russel Boakye) |

## Core workflows

1. **Discovery** — browse by geo tree, genre, talent type, rating.
2. **Onboarding** — talent verification; organizer credentials.
3. **Booking request** — calendar conflict check.
4. **Contract & payment** — terms and quote agreed and snapshotted. Contact details unlock at `agreed`. The organizer pays through Paystack; a signed webhook (verified server-side) moves the booking to `paid`. The talent's share settles to their subaccount; Agrofie's commission settles to Agrofie.
5. **Completion** — organizer confirms the event happened; dual review. Disputes attach to the booking.

## Money model

Agrofie is a marketplace, not a bank. **Agrofie never holds client funds.** Payments run through Paystack **subaccounts** (split payments):

1. Each talent connects a payout account (bank or mobile money, GHS). Agrofie creates a Paystack subaccount for them.
2. The organizer pays through Paystack Checkout. Agrofie initializes the transaction with the talent's subaccount and a flat commission (`transaction_charge`).
3. Paystack settles each side to its own account on its normal cycle (next day on the current account). Agrofie's commission goes to Agrofie; the talent's share goes to the talent. Nothing sits in an Agrofie-controlled balance.

**Not allowed:** delaying settlement to hold a talent's money until completion. That is escrow by another route and is the part with regulatory risk. If we ever want it, it needs its own legal review.

### What this costs and risks
| Item | Note |
| --- | --- |
| **Refunds** | Once a talent is settled, a refund comes out of Agrofie's main Paystack balance and Agrofie must recover it from the talent. Mitigate with a written refund policy (refunds via disputes, before the event), a booking-size cap at launch, a small reserve, and a clawback clause in the talent terms. **To confirm with Paystack: which balance a split-transaction refund is debited from.** |
| **Fee bearer** | **Decided: the talent bears Paystack's processing fee** (`bearer: subaccount`). The talent's net is their quote minus Agrofie's commission minus the processing fee, so the app must show talent that net before they accept a booking. The organizer pays one clean total and Agrofie's commission is not eroded by processing fees. |
| **Bypass** | Users could pay off-platform. Contact details stay hidden until `agreed`; disputes, refunds and reviews only apply to on-platform bookings. |
| **Approval** | Paystack must be comfortable with a talent marketplace using splits. **Ask Paystack support in writing.** The dashboard already offers subaccounts (GHS, bank or mobile money). |

### Revenue
| Phase | Revenue |
| --- | --- |
| **MVP** | Commission on each on-platform booking (flat, via split). Test mode with Paystack test keys until launch. |
| **Next** | **Booking protection** extras: contract e-sign, verified-talent guarantee, replacement-talent help. Verification fee as a possible pass-through. |
| **Later** | **Talent Pro** (featured placement, analytics, calendar sync) and **agency / venue plans** (teams, shortlists, invoices). |

Not legal advice. The MVP does not require a lawyer to start building; review terms, privacy (Ghana Data Protection Act registration) and the refund policy before taking real money at volume.

## Governance

- Mandatory media + identity verification for searchable talent.
- Real-time availability; high cancellation rates reduce search visibility.
- Dispute module with evidence (chat, contracts).

## Out of scope for launch

Phase two — organizers publishing events, talent claiming or pitching for them, multi-slot fill — is a separate product document: [PRODUCT_V2.md](./PRODUCT_V2.md) ([Google Doc](https://docs.google.com/document/d/199r-0Q4iiPrLDTUf4I4Zz5tWLzvt6fN4anNEGpq6xQo/edit)). Launch is organizer-to-talent discovery and booking only. v2 reuses that contract, payment, and review path; it is not a second marketplace.

## Scaffold vs later

| Now | Later (MVP+) | After launch (v2) |
| --- | --- | --- |
| Schema, seeds, health, role shells, **Kratos auth** | Discovery, profiles, verification, booking FSM, Paystack split payments (test mode → live) | Event listings, talent-side discovery |
| Lookup CMS stubs (admin-gated) | Full admin CMS, verification queue | Claim vs request/pitch, multi-slot events |
| Fake payment provider | Booking protection extras, Talent Pro, agency plans | Same split commission on event-originated bookings |
| Compose locally (incl. Kratos + MailHog) | DigitalOcean Kubernetes (DOKS) | Two-way entertainment marketplace |
