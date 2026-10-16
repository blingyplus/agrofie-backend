// Package dispute lets either party to an agreed-or-later booking flag a
// problem for admin mediation. Agrofie never holds funds (see
// docs/PRODUCT.md, Money model), so resolving a dispute records an
// outcome for the record — it does not itself move money or reopen a
// completed booking; any refund runs through Paystack support out of band.
package dispute

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput    = errors.New("invalid input")
	ErrForbidden       = errors.New("not allowed")
	ErrNotFound        = errors.New("booking not found")
	ErrNotDisputable   = errors.New("this booking can't be disputed yet")
	ErrAlreadyOpen     = errors.New("this booking already has an open dispute")
	ErrDisputeNotFound = errors.New("dispute not found")
)

// disputable are the booking statuses a dispute may be opened against. An
// inquiry has nothing agreed yet (withdraw it instead) and a cancelled
// booking has nothing left to mediate.
var disputable = map[string]bool{"agreed": true, "paid": true, "completed": true}

// resolutionStatuses are the statuses an admin may move a dispute to.
// "open" is the only way in (set by Open); it is never a resolve target.
var resolutionStatuses = map[string]bool{"under_review": true, "resolved": true, "closed": true}

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db beginner
}

func NewService(conn beginner) *Service {
	return &Service{db: conn}
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

type Dispute struct {
	ID             string
	BookingID      string
	ReasonCode     string
	ReasonName     string
	StatusCode     string
	StatusName     string
	Details        *string
	ResolutionNote *string
	OpenedByUserID string
	CreatedAt      time.Time
}

// AdminItem is a dispute plus enough booking context for the admin queue to
// triage without a follow-up lookup.
type AdminItem struct {
	Dispute
	OrganizerDisplayName string
	TalentDisplayName    string
	BookingStatusCode    string
	StartsAt             time.Time
}

// Open lets either party to a booking flag a problem once there is
// something to mediate (agreed, paid, or completed — not a bare inquiry,
// and not a booking that's already cancelled). Only one unresolved dispute
// is allowed per booking at a time.
func (s *Service) Open(ctx context.Context, callerUserID, bookingID, reasonCode string, details *string) (*Dispute, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	callerUUID, err := parseUUID(callerUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid caller id", ErrInvalidInput)
	}
	if reasonCode == "" {
		return nil, fmt.Errorf("%w: reason is required", ErrInvalidInput)
	}

	var out *Dispute
	err = s.withTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBookingForDispute(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(b.OrganizerUserID) != callerUserID && uuidString(b.TalentUserID) != callerUserID {
			return ErrForbidden
		}
		if !disputable[b.StatusCode] {
			return ErrNotDisputable
		}

		openCount, err := q.CountOpenDisputesForBooking(ctx, bID)
		if err != nil {
			return fmt.Errorf("count open disputes: %w", err)
		}
		if openCount > 0 {
			return ErrAlreadyOpen
		}

		row, err := q.InsertDispute(ctx, db.InsertDisputeParams{
			BookingID: bID, OpenedByUserID: callerUUID, ReasonCode: reasonCode, Details: details,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23502" {
				return fmt.Errorf("%w: unknown reason code", ErrInvalidInput)
			}
			return fmt.Errorf("insert dispute: %w", err)
		}

		full, err := q.GetDisputeByID(ctx, row.ID)
		if err != nil {
			return fmt.Errorf("reload dispute: %w", err)
		}
		out = toDispute(full)
		return nil
	})
	return out, err
}

// ListForBooking returns every dispute opened on a booking, newest first.
// Only the organizer or talent involved may see them — same visibility
// rule as the booking detail they're attached to.
func (s *Service) ListForBooking(ctx context.Context, callerUserID, bookingID string) ([]Dispute, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	var out []Dispute
	err = s.withTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBookingForDispute(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(b.OrganizerUserID) != callerUserID && uuidString(b.TalentUserID) != callerUserID {
			return ErrForbidden
		}
		rows, err := q.ListDisputesForBooking(ctx, bID)
		if err != nil {
			return fmt.Errorf("list disputes: %w", err)
		}
		out = make([]Dispute, 0, len(rows))
		for _, r := range rows {
			out = append(out, *toDispute(db.GetDisputeByIDRow(r)))
		}
		return nil
	})
	return out, err
}

// ListForAdmin returns every dispute, optionally filtered to one status,
// newest first. Admin-only.
func (s *Service) ListForAdmin(ctx context.Context, statusCode *string) ([]AdminItem, error) {
	var out []AdminItem
	err := s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListDisputesForAdmin(ctx, statusCode)
		if err != nil {
			return fmt.Errorf("list disputes: %w", err)
		}
		out = make([]AdminItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, AdminItem{
				Dispute: Dispute{
					ID: uuidString(r.ID), BookingID: uuidString(r.BookingID),
					ReasonCode: r.ReasonCode, ReasonName: r.ReasonName,
					StatusCode: r.StatusCode, StatusName: r.StatusName,
					Details: r.Details, ResolutionNote: r.ResolutionNote,
					OpenedByUserID: uuidString(r.OpenedByUserID), CreatedAt: r.CreatedAt.Time,
				},
				OrganizerDisplayName: r.OrganizerDisplayName,
				TalentDisplayName:    r.TalentDisplayName,
				BookingStatusCode:    r.BookingStatusCode,
				StartsAt:             r.StartsAt.Time,
			})
		}
		return nil
	})
	return out, err
}

// Resolve moves a dispute to under_review, resolved, or closed, with an
// optional note recording the outcome. It never touches the underlying
// booking or triggers a refund — Agrofie doesn't hold the funds to refund
// (see docs/PRODUCT.md, Money model); any money movement is a manual
// Paystack-side action taken alongside this record. Admin-only.
func (s *Service) Resolve(ctx context.Context, adminUserID, disputeID, statusCode string, resolutionNote *string) (*Dispute, error) {
	dID, err := parseUUID(disputeID)
	if err != nil {
		return nil, ErrDisputeNotFound
	}
	adminUUID, err := parseUUID(adminUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid admin id", ErrInvalidInput)
	}
	if !resolutionStatuses[statusCode] {
		return nil, fmt.Errorf("%w: status must be under_review, resolved, or closed", ErrInvalidInput)
	}

	var out *Dispute
	err = s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.GetDisputeForResolve(ctx, dID); errors.Is(err, pgx.ErrNoRows) {
			return ErrDisputeNotFound
		} else if err != nil {
			return fmt.Errorf("get dispute: %w", err)
		}

		if _, err := q.UpdateDisputeStatus(ctx, db.UpdateDisputeStatusParams{
			ID: dID, NewStatusCode: statusCode, ResolutionNote: resolutionNote, ResolvedByUserID: adminUUID,
		}); err != nil {
			return fmt.Errorf("update dispute: %w", err)
		}

		full, err := q.GetDisputeByID(ctx, dID)
		if err != nil {
			return fmt.Errorf("reload dispute: %w", err)
		}
		out = toDispute(full)
		return nil
	})
	return out, err
}

func toDispute(r db.GetDisputeByIDRow) *Dispute {
	return &Dispute{
		ID: uuidString(r.ID), BookingID: uuidString(r.BookingID),
		ReasonCode: r.ReasonCode, ReasonName: r.ReasonName,
		StatusCode: r.StatusCode, StatusName: r.StatusName,
		Details: r.Details, ResolutionNote: r.ResolutionNote,
		OpenedByUserID: uuidString(r.OpenedByUserID), CreatedAt: r.CreatedAt.Time,
	}
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
