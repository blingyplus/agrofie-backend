# Product v2 — Agrofie (Agorofie)

Strategic framework for a **two-way Ghanaian entertainment marketplace**.

Launch (v1) is one direction: organizers find verified talent and book them. Phase two opens the other: organizers publish work, and talent find, claim, or pitch for it. Together that is the product — a place the live entertainment industry actually runs on, not only a talent directory.

Source (this phase): [Agorofie Product v2](https://docs.google.com/document/d/199r-0Q4iiPrLDTUf4I4Zz5tWLzvt6fN4anNEGpq6xQo/edit)  
v1 source: [Agorofie High-Level Technical Specification](https://docs.google.com/document/d/1wSHOyVUSD6VyVjcUyLbr7lxt0r1nbOZMHNBrvPtyXtA/edit)  
This phase is **out of scope for launch**. See [PRODUCT.md](./PRODUCT.md).  
Spelling: product/docs often say **Agorofie**; repos/brand use **Agrofie**. Treat them as the same product.

## Table of contents

1. [Vision](#vision)
2. [Problem](#problem)
3. [Relationship to launch](#relationship-to-launch)
4. [Objectives](#objectives)
5. [Personas](#personas)
6. [How events work](#how-events-work)
7. [Core workflows](#core-workflows)
8. [Joining the booking path](#joining-the-booking-path)
9. [Business model](#business-model)
10. [Governance](#governance)
11. [What we will not do yet](#what-we-will-not-do-yet)
12. [How this evolves](#how-this-evolves)

## Vision

Agorofie should be where Ghana’s entertainment industry goes to work.

Not only “find me a Highlife band in Accra.” Also: “here is a wedding, a festival weekend, a hotel residency, a corporate night — who wants it.” Talent should be able to pick up a show from where they are. Organizers should be able to post ten of the same slot and fill them. Big jobs can ask for pitches. Small jobs can be claimed. Money, contract, and reputation stay on the same rails as launch.

That is a two-way marketplace. Demand and supply are both published. Matching is not a phone tree. The ambition is to be the default operating surface for live work — gigs you claim, gigs you pitch, many-slot events, private parties that need a shortlist, repeating corporate dates.

Launch starts strong with talent discovery. This phase is how the marketplace becomes a marketplace.

## Problem

Organizers hunting talent is only half the market.

Talent still waits to be found. Open work lives in WhatsApp groups, posters, and phone calls. There is no shared place to see what is on, pick up a slot nearby, or pitch for a larger show. An organizer who needs ten similar acts — or ten weekend slots at the same venue — has no way to publish capacity and let the market fill it.

Trust does not travel with those informal gigs. No verification gate, no contract snapshot, no fund hold, no review. The people who already struggle with discovery and payment in v1 still struggle when the work is posted the other way around.

## Relationship to launch

| | Launch (v1) | Phase two (this document) |
| --- | --- | --- |
| Who publishes | Talent (profile, rates, calendar) | Organizers (events, slots, requirements) |
| Who searches | Organizers | Talent (and organizers still search talent) |
| How work starts | Organizer requests a booking | Talent claims a slot, or pitches; organizer may still invite |
| How work closes | Contract → hold → complete → review | **The same close** |

v1 ships: verified directory, organizer-initiated booking, contract, application-level fund hold, completion, dual review. See [PRODUCT.md](./PRODUCT.md).

**v2 is out of scope for that launch.** It is not a second product and not a second payments system. It depends on verification, calendars, contracts, the escrow ledger, disputes, and reviews already working. We do not build event listings until that path is real.

## Objectives

1. **Event discovery for talent** — open work visible by region, category, date, and budget band, including “from where I am.”
2. **Two ways to take work** — instant claim when the organizer wants speed; request / pitch when they want to choose.
3. **Capacity, not one-off posts** — one listing can offer many of the same instance (ten slots, ten acts, ten nights).
4. **Same close as launch** — accepted work becomes a normal booking: terms, hold, completion, review.
5. **Industry gravity** — enough event types and matching modes that organizers and talent stay on the platform instead of going back to WhatsApp.

## Personas

| Role | What changes in v2 |
| --- | --- |
| Talent | Still receive inbound bookings. Also browse open events, claim a slot, or submit a pitch. Manage applications. Get paid the same way. |
| Organizer | Still search and book talent directly. Also post events, choose claim vs review, set how many slots, get notified when someone picks up a show, shortlist pitches. |
| Admin | Same identity/media queue and dispute mediation. Plus listing integrity: fake events, bait budgets, claim-and-drop. |

## How events work

An **event listing** is an organizer’s public (or restricted) offer of work. It is not a ticket for an audience. It is a job: what, when, where, what kind of talent, budget band or fixed fee, how many people or slots, and whether someone can take it immediately or must ask.

Talent who match the listing — verified, right category, in range — see it. Two fulfillment modes, which can coexist on the same marketplace and, if needed, on different slots of the same event:

### Instant claim

The organizer is filling a seat, not running a casting. Eligible talent takes the slot from where they are. The organizer is notified. That instance is filled. If it was the last open slot, the listing is no longer open.

Claiming is a real booking, not a handshake. Calendar conflicts still apply. The hold path starts so “I claimed it” means money and a contract, not a maybe.

### Request / pitch

The organizer wants to review. Talent apply with a short pitch and, when the listing allows, a proposed fee. The organizer shortlists, messages, accepts, or declines. Several people can request the same slot until one is accepted — or until the slot count is full.

This is the festival, headline, or “I need to see who you are” path.

### Many of the same instance

A festival weekend that needs ten similar acts. A venue that needs ten weekend slots. A corporate series that repeats. The organizer publishes **one listing with quantity** — e.g. ten — instead of ten unrelated posts. Each fill becomes its own booking. The listing can require a request for every slot, allow instant claim until the ten are gone, or mix: some seats open, some by review.

When someone picks up a slot, the organizer is notified. Remaining count drops. Overfill is not allowed.

## Core workflows

1. **Organizer creates a listing** — event type, date/window, venue or area, talent category, budget band or fixed fee, slot count, claim vs request. Draft until they open it.
2. **Talent discover work** — feed and filters (geo tree, genre/type, date, budget). Push when a matching listing opens near them.
3. **Talent takes work** — claim a slot, or submit a pitch/request.
4. **Organizer is notified** — claim: slot gone, booking started. Request: new application to review. They can accept, decline, or shortlist.
5. **Accepted work becomes a booking** — same contract, fund hold, completion, and review as an organizer-initiated booking.
6. **Listing life** — open → filling (some slots taken) → filled, or cancelled. Cancelled listings do not leave hanging claims.

Direct organizer-to-talent booking from v1 does not go away. An organizer can still find a specific act and book them. Listings are the other door.

## Joining the booking path

Once work is accepted, it is a **booking**. There is no parallel “event job” lifecycle.

- Calendar conflict check still runs.
- Quoted amount and terms are snapshotted (fixed claim price, or accepted pitch).
- Funds charged to the platform merchant and recorded in the hold ledger; release on completion.
- Dual review after the event.
- Disputes attach to the booking, with the listing as context.

Only verified, searchable talent take public listings. Organizer credentials still matter for posting. Taxonomy (genre, region, talent type, event type) stays lookup-driven — the client does not hardcode Accra/Highlife lists.

## Business model

- Commission on successful bookings, including those that started as a claim or an accepted pitch.
- Listings themselves are not charged at first. Taxing the post would starve liquidity while the reverse marketplace is empty.
- Later: promoted listings, featured events, organizer subscriptions / analytics — same “later” bucket as premium talent placement in v1.

## Governance

- Mandatory verification for talent who appear in search **or** take public event work.
- Claim-and-cancel and no-shows hit visibility the same way high cancellation does on inbound bookings.
- Instant claim should not be free talk: eligibility plus a prompt hold so a claim is binding.
- Bait listings, fake venues, and budget bait-and-switch are admin takedown.
- Multi-slot listings cannot assign more bookings than the published count.
- Disputes still need evidence (chat, contract, listing snapshot).

## What we will not do yet

Out of scope until launch booking and hold are real:

- Event listing product, talent event feed, claim/pitch flows.
- A separate payments or contract system for “event jobs.”
- Audience ticketing, door sales, or festival operating software.
- Unverified walk-up claims.
- Schema, API, or state-machine work for listings in the current scaffold.

Those belong after v1 discovery → booking → hold → complete is in production use.

## How this evolves

This is the large idea, not a closed feature list. The industry will ask for more ways to meet: private listings, agencies, repeating series, open calls for backing vs headliners, “work near me,” talent recommending other talent for a remaining slot.

Direction stays fixed:

- Both sides can publish.
- Both sides can take work.
- Claim for speed, pitch for selection, quantity for capacity.
- Money and reputation stay on the launch rails.

Start strong with finding talent. Then let talent find the work. That is how Agorofie becomes a leading entertainment marketplace rather than a directory with a checkout.
