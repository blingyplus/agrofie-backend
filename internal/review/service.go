// Package review is one-review-per-side after a booking completes: the
// organizer rates the talent, the talent rates the organizer. Reviews
// unlock only once booking.Service has moved a booking to completed.
package review

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
	ErrNotCompleted    = errors.New("booking is not completed yet")
	ErrAlreadyReviewed = errors.New("you've already reviewed this booking")
)

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

type Review struct {
	ID                string
	Rating            int
	Body              *string
	CreatedAt         time.Time
	AuthorUserID      string
	AuthorDisplayName string
}

// Submit leaves the caller's review of the other party on a completed
// booking. Each side may leave exactly one (booking_id, author_user_id) is
// unique) — a second attempt is ErrAlreadyReviewed, not silently ignored.
func (s *Service) Submit(ctx context.Context, authorUserID, bookingID string, rating int, body *string) (*Review, error) {
	if rating < 1 || rating > 5 {
		return nil, fmt.Errorf("%w: rating must be 1-5", ErrInvalidInput)
	}
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	authorUUID, err := parseUUID(authorUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid author id", ErrInvalidInput)
	}

	var out *Review
	err = s.withTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBookingForReview(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(b.OrganizerUserID) != authorUserID && uuidString(b.TalentUserID) != authorUserID {
			return ErrForbidden
		}
		if b.StatusCode != "completed" {
			return ErrNotCompleted
		}

		row, err := q.InsertReview(ctx, db.InsertReviewParams{
			BookingID: bID, AuthorUserID: authorUUID, Rating: int16(rating), Body: body,
		})
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrAlreadyReviewed
			}
			return fmt.Errorf("insert review: %w", err)
		}
		out = &Review{ID: uuidString(row.ID), Rating: rating, Body: body, CreatedAt: row.CreatedAt.Time, AuthorUserID: authorUserID}
		return nil
	})
	return out, err
}

// ListForBooking returns every review left on a booking (0, 1, or 2 rows).
// Only the organizer or talent involved may see them — same visibility
// rule as the booking detail they're attached to.
func (s *Service) ListForBooking(ctx context.Context, callerUserID, bookingID string) ([]Review, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	var out []Review
	err = s.withTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBookingForReview(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(b.OrganizerUserID) != callerUserID && uuidString(b.TalentUserID) != callerUserID {
			return ErrForbidden
		}
		rows, err := q.ListReviewsForBooking(ctx, bID)
		if err != nil {
			return fmt.Errorf("list reviews: %w", err)
		}
		out = make([]Review, 0, len(rows))
		for _, r := range rows {
			out = append(out, Review{
				ID: uuidString(r.ID), Rating: int(r.Rating), Body: r.Body, CreatedAt: r.CreatedAt.Time,
				AuthorUserID: uuidString(r.AuthorUserID), AuthorDisplayName: r.AuthorDisplayName,
			})
		}
		return nil
	})
	return out, err
}

// Summary is a talent's aggregate rating, safe to show publicly on their
// profile (only counts and an average — never review authorship at that
// level).
type Summary struct {
	ReviewCount   int
	AverageRating float64
}

// TalentSummary reads a talent's aggregate rating by their talent_profile
// id. No auth guard: this is public profile data, same as a talent's
// searchable listing.
func (s *Service) TalentSummary(ctx context.Context, talentProfileID string) (*Summary, error) {
	pID, err := parseUUID(talentProfileID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid talent id", ErrInvalidInput)
	}
	var out *Summary
	err = s.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetTalentReviewSummary(ctx, pID)
		if err != nil {
			return fmt.Errorf("get summary: %w", err)
		}
		out = &Summary{ReviewCount: int(row.ReviewCount), AverageRating: row.AverageRating}
		return nil
	})
	return out, err
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
