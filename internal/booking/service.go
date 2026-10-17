// Package booking is the inquiry -> (countered)* -> agreed -> paid flow: an
// organizer requests a slot, the talent accepts, declines or counters (and the
// two sides may trade up to MaxCounterRounds counter-offers, strictly taking
// turns), and once agreed the
// payments package (slice 6) drives the move to paid via MarkPaid after a
// verified Paystack webhook or reconciliation check.
package booking

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput      = errors.New("invalid input")
	ErrRateNotFound      = errors.New("talent has no matching published rate")
	ErrTalentNotBookable = errors.New("talent is not bookable")
	ErrNotAvailable      = errors.New("talent has not published availability for this range")
	ErrConflict          = errors.New("this slot is already agreed with another booking")
	ErrForbidden         = errors.New("not allowed")
	ErrNotPending        = errors.New("booking is not awaiting this action")
	ErrNotFound          = errors.New("booking not found")
	ErrEventNotYetOver   = errors.New("the event hasn't happened yet")
	ErrNotYourTurn       = errors.New("it's the other party's turn to respond to this booking")
	ErrMaxRounds         = errors.New("the maximum number of counter offers has been reached; accept, decline or cancel")
)

const (
	// MaxCounterRounds is how many counter-offers a booking may carry in total
	// (not counting the organizer's opening inquiry).
	MaxCounterRounds = 6
	// MaxAmountPesewas caps a proposed price at GHS 1,000,000.
	MaxAmountPesewas = 100_000_000
	maxNoteLen       = 500
)

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db    beginner
	avail *availability.Service

	allowEarlyComplete bool
}

// AllowEarlyCompletion lets bookings be completed before the event window has
// passed. For local end-to-end testing only (DEV_ALLOW_EARLY_COMPLETE=true).
func (s *Service) AllowEarlyCompletion(v bool) *Service {
	s.allowEarlyComplete = v
	return s
}

func NewService(conn beginner, avail *availability.Service) *Service {
	return &Service{db: conn, avail: avail}
}

func (s *Service) withTx(ctx context.Context, fn func(q *db.Queries) error) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(db.New(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

type RequestInput struct {
	TalentID      string
	EventTypeCode *string
	PlaceCode     *string
	StartsAt      time.Time
	EndsAt        time.Time
	VenueText     *string
	RateUnitCode  string
	Note          *string
}

type Booking struct {
	ID           string
	StatusCode   string
	StatusName   string
	StartsAt     time.Time
	EndsAt       time.Time
	VenueText    *string
	QuotedAmount string
	CurrencyCode string

	// Negotiation state: who made the last move, how many counters so far, and
	// whether the caller is the one who now has to respond.
	LastOfferByRole    string // "organizer" | "talent"
	CounterCount       int
	AwaitingMyResponse bool
}

// Offer is one immutable proposal in a booking's negotiation trail.
type Offer struct {
	ID            string
	Round         int
	ByRole        string // "organizer" | "talent"
	ByName        string
	ByMe          bool
	Amount        string
	AmountPesewas int64
	CurrencyCode  string
	StartsAt      time.Time
	EndsAt        time.Time
	Note          *string
	CreatedAt     time.Time
}

type CounterInput struct {
	BookingID     string
	AmountPesewas int64
	CurrencyCode  *string
	StartsAt      *time.Time
	EndsAt        *time.Time
	Note          *string
}

func live(status string) bool { return status == "inquiry" || status == "countered" }

func negotiation(status string, lastBy, organizer, caller pgtype.UUID, offerCount int32) (role string, counters int, awaiting bool) {
	role = "talent"
	if lastBy == organizer {
		role = "organizer"
	}
	counters = int(offerCount) - 1
	if counters < 0 {
		counters = 0
	}
	return role, counters, live(status) && lastBy != caller
}

type OrganizerBookingItem struct {
	Booking
	TalentDisplayName string
	TalentID          string
	EventTypeName     *string
}

type TalentBookingItem struct {
	Booking
	OrganizerDisplayName string
	OrganizerID          string
	EventTypeName        *string
}

type Detail struct {
	Booking
	OrganizerDisplayName string
	TalentDisplayName    string
	EventTypeName        *string
	TermsText            *string
	Offers               []Offer
}

// RequestBooking creates a new inquiry. It never trusts the client for
// price: the amount is looked up server-side from the talent's current
// published rate for RateUnitCode (+ EventTypeCode, if given) and snapshotted.
func (s *Service) RequestBooking(ctx context.Context, organizerUserID string, in RequestInput) (*Booking, error) {
	if !in.EndsAt.After(in.StartsAt) {
		return nil, fmt.Errorf("%w: ends must be after starts", ErrInvalidInput)
	}
	talentProfileID, err := parseUUID(in.TalentID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid talent id", ErrInvalidInput)
	}

	var out *Booking
	err = s.withTx(ctx, func(q *db.Queries) error {
		bookable, err := q.CheckTalentBookable(ctx, talentProfileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: unknown talent", ErrInvalidInput)
		}
		if err != nil {
			return fmt.Errorf("check bookable: %w", err)
		}
		if !bookable.IsSearchable || !bookable.HasPayout {
			return ErrTalentNotBookable
		}

		rate, err := q.GetCurrentRateForBooking(ctx, db.GetCurrentRateForBookingParams{
			TalentProfileID: talentProfileID, Code: in.RateUnitCode, EventTypeCode: in.EventTypeCode,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRateNotFound
		}
		if err != nil {
			return fmt.Errorf("get rate: %w", err)
		}

		// Availability check runs against the shared pool, not this
		// transaction — it's a read of a separate service's data, and a
		// pending write here shouldn't need to see it. The authoritative
		// guard against double-booking is the conflict count below, inside
		// this transaction.
		ok, err := s.avail.IsRangeAvailable(ctx, uuidString(talentProfileID), in.StartsAt, in.EndsAt)
		if err != nil {
			return fmt.Errorf("check availability: %w", err)
		}
		if !ok {
			return ErrNotAvailable
		}

		conflicts, err := q.CountConflictingAgreedBookings(ctx, db.CountConflictingAgreedBookingsParams{
			TalentProfileID: talentProfileID,
			NewStartsAt:     pgtype.Timestamptz{Time: in.StartsAt, Valid: true},
			NewEndsAt:       pgtype.Timestamptz{Time: in.EndsAt, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("check conflicts: %w", err)
		}
		if conflicts > 0 {
			return ErrConflict
		}

		organizerUUID, err := parseUUID(organizerUserID)
		if err != nil {
			return fmt.Errorf("%w: invalid organizer id", ErrInvalidInput)
		}
		row, err := q.InsertBooking(ctx, db.InsertBookingParams{
			OrganizerUserID: organizerUUID,
			TalentProfileID: talentProfileID,
			StartsAt:        pgtype.Timestamptz{Time: in.StartsAt, Valid: true},
			EndsAt:          pgtype.Timestamptz{Time: in.EndsAt, Valid: true},
			VenueText:       in.VenueText,
			QuotedAmount:    mustNumeric(rate.Amount),
			CurrencyCode:    &rate.CurrencyCode,
			EventTypeCode:   in.EventTypeCode,
			PlaceCode:       in.PlaceCode,
		})
		if err != nil {
			return fmt.Errorf("insert booking: %w", err)
		}
		if err := q.InsertInitialBookingOffer(ctx, db.InsertInitialBookingOfferParams{ID: row.ID, Note: in.Note}); err != nil {
			return fmt.Errorf("insert initial offer: %w", err)
		}
		if err := q.InsertBookingStatusEvent(ctx, db.InsertBookingStatusEventParams{
			BookingID: row.ID, StatusCode: "inquiry", ActorUserID: organizerUUID, Note: in.Note,
		}); err != nil {
			return fmt.Errorf("insert status event: %w", err)
		}
		out = &Booking{
			ID: uuidString(row.ID), StatusCode: "inquiry", StatusName: "Inquiry",
			StartsAt: in.StartsAt, EndsAt: in.EndsAt, VenueText: in.VenueText,
			QuotedAmount: rate.Amount, CurrencyCode: rate.CurrencyCode,
			LastOfferByRole: "organizer",
		}
		return nil
	})
	return out, err
}

// callerRole reports whether the caller is the booking's organizer or its
// talent, or ErrForbidden if they are neither.
func callerRole(b db.GetBookingForTransitionRow, caller string) (string, error) {
	switch caller {
	case uuidString(b.OrganizerUserID):
		return "organizer", nil
	case uuidString(b.TalentUserID):
		return "talent", nil
	}
	return "", ErrForbidden
}

// requireTurn enforces the negotiation state machine for the responding
// party: the booking must be live and the caller must NOT be the one who
// made the last move.
func requireTurn(b db.GetBookingForTransitionRow, caller string) error {
	if _, err := callerRole(b, caller); err != nil {
		return err
	}
	if !live(b.StatusCode) {
		return ErrNotPending
	}
	if uuidString(b.LastOfferByUserID) == caller {
		return ErrNotYourTurn
	}
	return nil
}

// CounterBooking lets either party counter a live booking on their turn: a
// new price (pesewas, booking currency) and optionally new event dates and a
// note. The proposal is stored as an immutable offer; the booking's own
// quoted terms mirror it until someone accepts.
func (s *Service) CounterBooking(ctx context.Context, callerUserID string, in CounterInput) (*Booking, error) {
	bID, err := parseUUID(in.BookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	callerUUID, err := parseUUID(callerUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	if in.AmountPesewas <= 0 || in.AmountPesewas > MaxAmountPesewas {
		return nil, fmt.Errorf("%w: amount must be between GHS 0.01 and GHS %d", ErrInvalidInput, MaxAmountPesewas/100)
	}
	if (in.StartsAt == nil) != (in.EndsAt == nil) {
		return nil, fmt.Errorf("%w: provide both startsAt and endsAt, or neither", ErrInvalidInput)
	}
	note := in.Note
	if note != nil {
		t := strings.TrimSpace(*note)
		if len(t) > maxNoteLen {
			return nil, fmt.Errorf("%w: note is too long (max %d characters)", ErrInvalidInput, maxNoteLen)
		}
		if t == "" {
			note = nil
		} else {
			note = &t
		}
	}

	var out *Booking
	err = s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.LockBookingForCounter(ctx, bID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock booking: %w", err)
		}
		b, err := q.GetBookingForTransition(ctx, bID)
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if err := requireTurn(b, callerUserID); err != nil {
			return err
		}
		if int(b.LastRound)-1 >= MaxCounterRounds {
			return ErrMaxRounds
		}
		cur, err := q.GetBookingCurrency(ctx, bID)
		if err != nil {
			return fmt.Errorf("get currency: %w", err)
		}
		if in.CurrencyCode != nil && !strings.EqualFold(*in.CurrencyCode, cur) {
			return fmt.Errorf("%w: offers must be in %s", ErrInvalidInput, cur)
		}

		startsAt, endsAt := b.StartsAt.Time, b.EndsAt.Time
		if in.StartsAt != nil {
			startsAt, endsAt = *in.StartsAt, *in.EndsAt
			if !endsAt.After(startsAt) {
				return fmt.Errorf("%w: ends must be after starts", ErrInvalidInput)
			}
			if !startsAt.Equal(b.StartsAt.Time) || !endsAt.Equal(b.EndsAt.Time) {
				if !startsAt.After(time.Now()) {
					return fmt.Errorf("%w: event date must be in the future", ErrInvalidInput)
				}
				ok, err := s.avail.IsRangeAvailable(ctx, uuidString(b.TalentProfileID), startsAt, endsAt)
				if err != nil {
					return fmt.Errorf("check availability: %w", err)
				}
				if !ok {
					return ErrNotAvailable
				}
				conflicts, err := q.CountConflictingAgreedBookings(ctx, db.CountConflictingAgreedBookingsParams{
					TalentProfileID: b.TalentProfileID,
					NewStartsAt:     pgtype.Timestamptz{Time: startsAt, Valid: true},
					NewEndsAt:       pgtype.Timestamptz{Time: endsAt, Valid: true},
					ExcludeID:       b.ID,
				})
				if err != nil {
					return fmt.Errorf("check conflicts: %w", err)
				}
				if conflicts > 0 {
					return ErrConflict
				}
			}
		}

		amount := mustNumeric(fmt.Sprintf("%d.%02d", in.AmountPesewas/100, in.AmountPesewas%100))
		startTS := pgtype.Timestamptz{Time: startsAt, Valid: true}
		endTS := pgtype.Timestamptz{Time: endsAt, Valid: true}
		rows, err := q.ApplyBookingCounter(ctx, db.ApplyBookingCounterParams{ID: bID, Amount: amount, StartsAt: startTS, EndsAt: endTS})
		if err != nil {
			return fmt.Errorf("apply counter: %w", err)
		}
		if rows != 1 {
			return ErrNotPending
		}
		if err := q.InsertBookingOffer(ctx, db.InsertBookingOfferParams{
			BookingID: bID, Round: b.LastRound + 1, OfferedByUserID: callerUUID,
			Amount: amount, CurrencyCode: cur, StartsAt: startTS, EndsAt: endTS, Note: note,
		}); err != nil {
			return fmt.Errorf("insert offer: %w", err)
		}
		if err := q.InsertBookingStatusEvent(ctx, db.InsertBookingStatusEventParams{
			BookingID: bID, StatusCode: "countered", ActorUserID: callerUUID, Note: note,
		}); err != nil {
			return fmt.Errorf("insert status event: %w", err)
		}
		role, _ := callerRole(b, callerUserID)
		out = &Booking{
			ID: uuidString(bID), StatusCode: "countered", StatusName: "Countered",
			StartsAt: startsAt, EndsAt: endsAt, QuotedAmount: fmt.Sprintf("%d.%02d", in.AmountPesewas/100, in.AmountPesewas%100),
			CurrencyCode: cur, LastOfferByRole: role, CounterCount: int(b.LastRound), AwaitingMyResponse: false,
		}
		return nil
	})
	return out, err
}

// AcceptBooking moves a live booking (inquiry or countered) to agreed and
// snapshots the contract terms from the latest offer. Only the party who did
// NOT make the last move can accept.
func (s *Service) AcceptBooking(ctx context.Context, callerUserID, bookingID string) (*Booking, error) {
	talentUserID := callerUserID
	return s.transition(ctx, bookingID, "agreed", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if err := requireTurn(b, callerUserID); err != nil {
			return err
		}
		// Re-check: another inquiry for an overlapping slot may have been
		// accepted since this one was requested.
		conflicts, err := q.CountConflictingAgreedBookings(ctx, db.CountConflictingAgreedBookingsParams{
			TalentProfileID: b.TalentProfileID, NewStartsAt: b.StartsAt, NewEndsAt: b.EndsAt, ExcludeID: b.ID,
		})
		if err != nil {
			return fmt.Errorf("check conflicts: %w", err)
		}
		if conflicts > 0 {
			return ErrConflict
		}
		return nil
	}, func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		price, err := q.GetBookingPrice(ctx, b.ID)
		if err != nil {
			return fmt.Errorf("get price: %w", err)
		}
		terms := fmt.Sprintf(
			"Booking agreed on %s.\nAgreed price: %s %s.\nEvent window: %s to %s.\nQuoted amount and terms are fixed as of this agreement (after %d counter-offer(s)) and do not change if rates are later edited.",
			time.Now().UTC().Format(time.RFC3339), price.CurrencyCode, price.Amount,
			b.StartsAt.Time.Format(time.RFC3339), b.EndsAt.Time.Format(time.RFC3339), int(b.LastRound)-1,
		)
		return q.InsertContract(ctx, db.InsertContractParams{BookingID: b.ID, TermsText: terms})
	}, talentUserID)
}

// DeclineBooking lets the talent turn down a live booking (inquiry or
// counter-offer) when it is their turn to respond.
func (s *Service) DeclineBooking(ctx context.Context, talentUserID, bookingID string) (*Booking, error) {
	reason := "talent_cancel"
	return s.transition(ctx, bookingID, "cancelled", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if uuidString(b.TalentUserID) != talentUserID {
			return ErrForbidden
		}
		return requireTurn(b, talentUserID)
	}, nil, talentUserID, &reason)
}

// CancelBooking lets the organizer withdraw their own live booking (inquiry
// or countered, whoever's turn it is). Once a booking is agreed, cancelling
// goes through a dispute instead.
func (s *Service) CancelBooking(ctx context.Context, organizerUserID, bookingID string) (*Booking, error) {
	reason := "organizer_cancel"
	return s.transition(ctx, bookingID, "cancelled", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if uuidString(b.OrganizerUserID) != organizerUserID {
			return ErrForbidden
		}
		if !live(b.StatusCode) {
			return ErrNotPending
		}
		return nil
	}, nil, organizerUserID, &reason)
}

// CompleteBooking moves a paid booking to completed. Either party to the
// booking may confirm it, and only once the event window has actually
// passed — completion is what unlocks reviews, so it shouldn't be
// backdated to before the event happened.
func (s *Service) CompleteBooking(ctx context.Context, callerUserID, bookingID string) (*Booking, error) {
	return s.transition(ctx, bookingID, "completed", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if uuidString(b.OrganizerUserID) != callerUserID {
			myProfileID, err := profileIDForUser(ctx, q, callerUserID)
			if err != nil {
				if errors.Is(err, errNotTalent) {
					return ErrForbidden
				}
				return err
			}
			if uuidString(myProfileID) != uuidString(b.TalentProfileID) {
				return ErrForbidden
			}
		}
		if b.StatusCode != "paid" {
			return ErrNotPending
		}
		if !s.allowEarlyComplete && !time.Now().After(b.EndsAt.Time) {
			return ErrEventNotYetOver
		}
		return nil
	}, nil, callerUserID)
}

// MarkPaid moves an agreed booking to paid. It is driven by the payments
// package (a verified Paystack webhook or reconciliation check), never by
// the client directly — so there is no user actor: booking_status_events
// .actor_user_id is left null for this transition ("system").
func (s *Service) MarkPaid(ctx context.Context, bookingID string) (*Booking, error) {
	return s.transitionAny(ctx, bookingID, "paid", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if b.StatusCode != "agreed" {
			return ErrNotPending
		}
		return nil
	}, nil, pgtype.UUID{})
}

// transition runs the shared guard-then-write pattern every state change
// uses: load the booking, let guard veto it, apply the DB transition
// (checked against the expected prior status so a race loses cleanly), run
// an optional side effect, and log the status event.
func (s *Service) transition(
	ctx context.Context, bookingID, newStatus string,
	guard func(q *db.Queries, b db.GetBookingForTransitionRow) error,
	after func(q *db.Queries, b db.GetBookingForTransitionRow) error,
	actorUserID string, cancellationReason ...*string,
) (*Booking, error) {
	actorUUID, err := parseUUID(actorUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid actor id", ErrInvalidInput)
	}
	return s.transitionAny(ctx, bookingID, newStatus, guard, after, actorUUID, cancellationReason...)
}

// transitionAny is transition with an already-resolved actor, which may be
// invalid/null for a system-driven transition (see MarkPaid).
func (s *Service) transitionAny(
	ctx context.Context, bookingID, newStatus string,
	guard func(q *db.Queries, b db.GetBookingForTransitionRow) error,
	after func(q *db.Queries, b db.GetBookingForTransitionRow) error,
	actorUUID pgtype.UUID, cancellationReason ...*string,
) (*Booking, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}

	var out *Booking
	err = s.withTx(ctx, func(q *db.Queries) error {
		// Row lock: an accept can't slip in between a counter's turn check and write.
		if _, err := q.LockBookingForCounter(ctx, bID); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("lock booking: %w", err)
		}
		b, err := q.GetBookingForTransition(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if err := guard(q, b); err != nil {
			return err
		}

		var reason *string
		if len(cancellationReason) > 0 {
			reason = cancellationReason[0]
		}
		rows, err := q.UpdateBookingStatus(ctx, db.UpdateBookingStatusParams{
			ID: bID, NewStatusCode: newStatus, ExpectedStatusCode: b.StatusCode, CancellationReasonCode: reason,
		})
		if err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		if rows != 1 {
			return ErrNotPending // lost a race with another transition
		}

		if after != nil {
			if err := after(q, b); err != nil {
				return err
			}
		}
		if err := q.InsertBookingStatusEvent(ctx, db.InsertBookingStatusEventParams{
			BookingID: bID, StatusCode: newStatus, ActorUserID: actorUUID,
		}); err != nil {
			return fmt.Errorf("insert status event: %w", err)
		}

		out = &Booking{ID: uuidString(bID), StatusCode: newStatus, StartsAt: b.StartsAt.Time, EndsAt: b.EndsAt.Time}
		return nil
	})
	return out, err
}

func (s *Service) ListMyBookingsAsOrganizer(ctx context.Context, organizerUserID string) ([]OrganizerBookingItem, error) {
	uid, err := parseUUID(organizerUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	var out []OrganizerBookingItem
	err = s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListMyBookingsAsOrganizer(ctx, uid)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		out = make([]OrganizerBookingItem, 0, len(rows))
		for _, r := range rows {
			role, counters, awaiting := negotiation(r.StatusCode, r.LastOfferByUserID, uid, uid, r.OfferCount)
			out = append(out, OrganizerBookingItem{
				Booking: Booking{
					ID: uuidString(r.ID), StatusCode: r.StatusCode, StatusName: r.StatusName,
					StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, VenueText: r.VenueText,
					QuotedAmount: r.QuotedAmount, CurrencyCode: strOr(r.CurrencyCode),
					LastOfferByRole: role, CounterCount: counters, AwaitingMyResponse: awaiting,
				},
				TalentDisplayName: r.TalentDisplayName, TalentID: uuidString(r.TalentProfileID), EventTypeName: r.EventTypeName,
			})
		}
		return nil
	})
	return out, err
}

func (s *Service) ListMyBookingsAsTalent(ctx context.Context, talentUserID string) ([]TalentBookingItem, error) {
	talentUUID, err := parseUUID(talentUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	var out []TalentBookingItem
	err = s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, talentUserID)
		if err != nil {
			if errors.Is(err, errNotTalent) {
				return ErrForbidden
			}
			return err
		}
		rows, err := q.ListMyBookingsAsTalent(ctx, profileID)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		out = make([]TalentBookingItem, 0, len(rows))
		for _, r := range rows {
			role, counters, awaiting := negotiation(r.StatusCode, r.LastOfferByUserID, r.OrganizerUserID, talentUUID, r.OfferCount)
			out = append(out, TalentBookingItem{
				Booking: Booking{
					ID: uuidString(r.ID), StatusCode: r.StatusCode, StatusName: r.StatusName,
					StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, VenueText: r.VenueText,
					QuotedAmount: r.QuotedAmount, CurrencyCode: strOr(r.CurrencyCode),
					LastOfferByRole: role, CounterCount: counters, AwaitingMyResponse: awaiting,
				},
				OrganizerDisplayName: r.OrganizerDisplayName, OrganizerID: uuidString(r.OrganizerUserID), EventTypeName: r.EventTypeName,
			})
		}
		return nil
	})
	return out, err
}

// GetDetail returns the full booking, including the contract terms once
// agreed. Only the organizer who made it or the talent it's for can see it.
func (s *Service) GetDetail(ctx context.Context, callerUserID, bookingID string) (*Detail, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	var out *Detail
	err = s.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetBookingDetail(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get detail: %w", err)
		}
		if uuidString(row.OrganizerUserID) != callerUserID && uuidString(row.TalentUserID) != callerUserID {
			return ErrForbidden
		}
		callerUUID, err := parseUUID(callerUserID)
		if err != nil {
			return ErrForbidden
		}
		role, counters, awaiting := negotiation(row.StatusCode, row.LastOfferByUserID, row.OrganizerUserID, callerUUID, row.OfferCount)
		offerRows, err := q.ListBookingOffers(ctx, bID)
		if err != nil {
			return fmt.Errorf("list offers: %w", err)
		}
		offers := make([]Offer, 0, len(offerRows))
		for _, o := range offerRows {
			byRole := "talent"
			if o.ByOrganizer {
				byRole = "organizer"
			}
			offers = append(offers, Offer{
				ID: uuidString(o.ID), Round: int(o.Round), ByRole: byRole, ByName: o.OfferedByName,
				ByMe: uuidString(o.OfferedByUserID) == callerUserID, Amount: o.Amount, AmountPesewas: o.AmountPesewas,
				CurrencyCode: o.CurrencyCode, StartsAt: o.StartsAt.Time, EndsAt: o.EndsAt.Time, Note: o.Note, CreatedAt: o.CreatedAt.Time,
			})
		}
		out = &Detail{
			Booking: Booking{
				ID: uuidString(row.ID), StatusCode: row.StatusCode, StatusName: row.StatusName,
				StartsAt: row.StartsAt.Time, EndsAt: row.EndsAt.Time, VenueText: row.VenueText,
				QuotedAmount: row.QuotedAmount, CurrencyCode: strOr(row.CurrencyCode),
				LastOfferByRole: role, CounterCount: counters, AwaitingMyResponse: awaiting,
			},
			OrganizerDisplayName: row.OrganizerDisplayName, TalentDisplayName: row.TalentDisplayName,
			EventTypeName: row.EventTypeName, TermsText: row.TermsText, Offers: offers,
		}
		return nil
	})
	return out, err
}

var errNotTalent = errors.New("caller has no talent profile")

func profileIDForUser(ctx context.Context, q *db.Queries, userID string) (pgtype.UUID, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	id, err := q.GetTalentProfileIDByUserID(ctx, pgtype.UUID{Bytes: uid, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, errNotTalent
	}
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("look up talent profile: %w", err)
	}
	return id, nil
}

func parseUUID(s string) (pgtype.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid id %q: %w", s, err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func uuidString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mustNumeric(decimal string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(decimal)
	return n
}
