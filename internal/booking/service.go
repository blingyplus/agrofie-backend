// Package booking is the inquiry -> agreed -> paid flow: an organizer
// requests a slot, the talent accepts or declines, and once agreed the
// payments package (slice 6) drives the move to paid via MarkPaid after a
// verified Paystack webhook or reconciliation check.
package booking

import (
	"context"
	"errors"
	"fmt"
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
)

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db    beginner
	avail *availability.Service
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
		if err := q.InsertBookingStatusEvent(ctx, db.InsertBookingStatusEventParams{
			BookingID: row.ID, StatusCode: "inquiry", ActorUserID: organizerUUID, Note: in.Note,
		}); err != nil {
			return fmt.Errorf("insert status event: %w", err)
		}
		out = &Booking{
			ID: uuidString(row.ID), StatusCode: "inquiry", StatusName: "Inquiry",
			StartsAt: in.StartsAt, EndsAt: in.EndsAt, VenueText: in.VenueText,
			QuotedAmount: rate.Amount, CurrencyCode: rate.CurrencyCode,
		}
		return nil
	})
	return out, err
}

// AcceptBooking moves an inquiry to agreed and snapshots the contract terms.
// Only the talent the booking is for can accept it.
func (s *Service) AcceptBooking(ctx context.Context, talentUserID, bookingID string) (*Booking, error) {
	return s.transition(ctx, bookingID, "agreed", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		myProfileID, err := profileIDForUser(ctx, q, talentUserID)
		if err != nil {
			if errors.Is(err, errNotTalent) {
				return ErrForbidden
			}
			return err
		}
		if uuidString(myProfileID) != uuidString(b.TalentProfileID) {
			return ErrForbidden
		}
		if b.StatusCode != "inquiry" {
			return ErrNotPending
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
		terms := fmt.Sprintf(
			"Booking agreed on %s.\nEvent window: %s to %s.\nQuoted amount and terms are fixed as of this agreement and do not change if rates are later edited.",
			time.Now().UTC().Format(time.RFC3339), b.StartsAt.Time.Format(time.RFC3339), b.EndsAt.Time.Format(time.RFC3339),
		)
		return q.InsertContract(ctx, db.InsertContractParams{BookingID: b.ID, TermsText: terms})
	}, talentUserID)
}

// DeclineBooking lets the talent turn down a pending inquiry.
func (s *Service) DeclineBooking(ctx context.Context, talentUserID, bookingID string) (*Booking, error) {
	reason := "talent_cancel"
	return s.transition(ctx, bookingID, "cancelled", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		myProfileID, err := profileIDForUser(ctx, q, talentUserID)
		if err != nil {
			if errors.Is(err, errNotTalent) {
				return ErrForbidden
			}
			return err
		}
		if uuidString(myProfileID) != uuidString(b.TalentProfileID) {
			return ErrForbidden
		}
		if b.StatusCode != "inquiry" {
			return ErrNotPending
		}
		return nil
	}, nil, talentUserID, &reason)
}

// CancelBooking lets the organizer withdraw their own inquiry. Once a
// booking is agreed, cancelling goes through a dispute instead.
func (s *Service) CancelBooking(ctx context.Context, organizerUserID, bookingID string) (*Booking, error) {
	reason := "organizer_cancel"
	return s.transition(ctx, bookingID, "cancelled", func(q *db.Queries, b db.GetBookingForTransitionRow) error {
		if uuidString(b.OrganizerUserID) != organizerUserID {
			return ErrForbidden
		}
		if b.StatusCode != "inquiry" {
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
		if !time.Now().After(b.EndsAt.Time) {
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
			out = append(out, OrganizerBookingItem{
				Booking: Booking{
					ID: uuidString(r.ID), StatusCode: r.StatusCode, StatusName: r.StatusName,
					StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, VenueText: r.VenueText,
					QuotedAmount: r.QuotedAmount, CurrencyCode: strOr(r.CurrencyCode),
				},
				TalentDisplayName: r.TalentDisplayName, TalentID: uuidString(r.TalentProfileID), EventTypeName: r.EventTypeName,
			})
		}
		return nil
	})
	return out, err
}

func (s *Service) ListMyBookingsAsTalent(ctx context.Context, talentUserID string) ([]TalentBookingItem, error) {
	var out []TalentBookingItem
	err := s.withTx(ctx, func(q *db.Queries) error {
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
			out = append(out, TalentBookingItem{
				Booking: Booking{
					ID: uuidString(r.ID), StatusCode: r.StatusCode, StatusName: r.StatusName,
					StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, VenueText: r.VenueText,
					QuotedAmount: r.QuotedAmount, CurrencyCode: strOr(r.CurrencyCode),
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
		out = &Detail{
			Booking: Booking{
				ID: uuidString(row.ID), StatusCode: row.StatusCode, StatusName: row.StatusName,
				StartsAt: row.StartsAt.Time, EndsAt: row.EndsAt.Time, VenueText: row.VenueText,
				QuotedAmount: row.QuotedAmount, CurrencyCode: strOr(row.CurrencyCode),
			},
			OrganizerDisplayName: row.OrganizerDisplayName, TalentDisplayName: row.TalentDisplayName,
			EventTypeName: row.EventTypeName, TermsText: row.TermsText,
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
