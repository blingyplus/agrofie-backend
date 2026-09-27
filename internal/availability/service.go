// Package availability lets a talent publish the windows they're free (and
// carve out blackout dates inside them), and lets the booking flow (and
// organizers browsing a profile) check whether a date range is bookable.
package availability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidRange = errors.New("ends must be after starts")
	ErrNotTalent    = errors.New("caller has no talent profile")
	ErrNotFound     = errors.New("availability block not found")
	ErrInvalidInput = errors.New("invalid input")
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

type AddBlockInput struct {
	StartsAt    time.Time
	EndsAt      time.Time
	IsAvailable bool
	Note        *string
}

type Block struct {
	ID          string
	StartsAt    time.Time
	EndsAt      time.Time
	IsAvailable bool
	Note        *string // never set on Window (the public-safe shape)
}

// Window is the public-safe shape: no note, since a talent's own reason for
// a blackout (e.g. "unwell") is not for organizers to see.
type Window struct {
	ID          string
	StartsAt    time.Time
	EndsAt      time.Time
	IsAvailable bool
}

// AddBlock publishes a window (available or a blackout) for the caller's own
// talent profile.
func (s *Service) AddBlock(ctx context.Context, userID string, in AddBlockInput) (*Block, error) {
	if !in.EndsAt.After(in.StartsAt) {
		return nil, ErrInvalidRange
	}
	var out *Block
	err := s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		row, err := q.InsertAvailabilityBlock(ctx, db.InsertAvailabilityBlockParams{
			TalentProfileID: profileID,
			StartsAt:        pgtype.Timestamptz{Time: in.StartsAt, Valid: true},
			EndsAt:          pgtype.Timestamptz{Time: in.EndsAt, Valid: true},
			IsAvailable:     in.IsAvailable,
			Note:            in.Note,
		})
		if err != nil {
			return fmt.Errorf("insert block: %w", err)
		}
		out = &Block{
			ID: uuidString(row.ID), StartsAt: row.StartsAt.Time, EndsAt: row.EndsAt.Time,
			IsAvailable: row.IsAvailable, Note: row.Note,
		}
		return nil
	})
	return out, err
}

// RemoveBlock deletes a block the caller owns. Deleting someone else's block
// (or an unknown id) fails with ErrNotFound — the caller can't tell which,
// which is the point.
func (s *Service) RemoveBlock(ctx context.Context, userID, blockID string) error {
	bID, err := parseUUID(blockID)
	if err != nil {
		return ErrNotFound
	}
	return s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		rows, err := q.DeleteAvailabilityBlock(ctx, db.DeleteAvailabilityBlockParams{ID: bID, TalentProfileID: profileID})
		if err != nil {
			return fmt.Errorf("delete block: %w", err)
		}
		if rows != 1 {
			return ErrNotFound
		}
		return nil
	})
}

// ListMine returns the caller's own blocks (with notes) overlapping [from, to).
func (s *Service) ListMine(ctx context.Context, userID string, from, to time.Time) ([]Block, error) {
	var out []Block
	err := s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		rows, err := q.ListMyAvailabilityBlocks(ctx, db.ListMyAvailabilityBlocksParams{
			TalentProfileID: profileID,
			FromTs:          pgtype.Timestamptz{Time: from, Valid: true},
			ToTs:            pgtype.Timestamptz{Time: to, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("list mine: %w", err)
		}
		out = make([]Block, 0, len(rows))
		for _, r := range rows {
			out = append(out, Block{ID: uuidString(r.ID), StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, IsAvailable: r.IsAvailable, Note: r.Note})
		}
		return nil
	})
	return out, err
}

// ListForTalent is the public-safe view used on a talent's profile: windows
// only, no notes.
func (s *Service) ListForTalent(ctx context.Context, talentProfileID string, from, to time.Time) ([]Window, error) {
	pID, err := parseUUID(talentProfileID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid talent id", ErrInvalidInput)
	}
	var out []Window
	err = s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListTalentAvailabilityWindows(ctx, db.ListTalentAvailabilityWindowsParams{
			TalentProfileID: pID,
			FromTs:          pgtype.Timestamptz{Time: from, Valid: true},
			ToTs:            pgtype.Timestamptz{Time: to, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("list for talent: %w", err)
		}
		out = make([]Window, 0, len(rows))
		for _, r := range rows {
			out = append(out, Window{ID: uuidString(r.ID), StartsAt: r.StartsAt.Time, EndsAt: r.EndsAt.Time, IsAvailable: r.IsAvailable})
		}
		return nil
	})
	return out, err
}

// IsRangeAvailable reports whether [from, to) can be booked: it must be
// fully covered by a single published available block, and not overlapped
// by any blackout block. This is a deliberate simplification — it does not
// union adjacent available blocks — good enough for one talent publishing
// "available this whole month" and carving out exceptions, which is the
// only pattern the app's calendar UI produces today.
func (s *Service) IsRangeAvailable(ctx context.Context, talentProfileID string, from, to time.Time) (bool, error) {
	windows, err := s.ListForTalent(ctx, talentProfileID, from, to)
	if err != nil {
		return false, err
	}
	coveredByAvailable := false
	for _, w := range windows {
		if w.IsAvailable && !w.StartsAt.After(from) && !w.EndsAt.Before(to) {
			coveredByAvailable = true
		}
		if !w.IsAvailable && w.StartsAt.Before(to) && w.EndsAt.After(from) {
			return false, nil // a blackout overlapping the range always wins
		}
	}
	return coveredByAvailable, nil
}

func profileIDForUser(ctx context.Context, q *db.Queries, userID string) (pgtype.UUID, error) {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	id, err := q.GetTalentProfileIDByUserID(ctx, pgtype.UUID{Bytes: uid, Valid: true})
	if errors.Is(err, pgx.ErrNoRows) {
		return pgtype.UUID{}, ErrNotTalent
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
