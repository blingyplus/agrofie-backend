// Package talentrequest lets an organizer pitch a "Tell us what you need"
// request when Discover has nothing suitable. Requests are visible to the
// organizer who made them and to admins only — talent are never notified or
// shown them (that is a later, v2 concern). Status is a lookup table.
package talentrequest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrNotFound     = errors.New("request not found")
)

const (
	maxNoteLen       = 1000
	maxBudgetPesewas = 100_000_000
)

// adminStatuses are the statuses an admin may move a request to. "open" is
// the way in (set on create) and can be restored.
var adminStatuses = map[string]bool{"open": true, "matched": true, "closed": true}

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct{ db beginner }

func NewService(conn beginner) *Service { return &Service{db: conn} }

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

type Input struct {
	EventTypeCode *string
	GenreCode     *string
	PlaceCode     *string
	EventDate     *string // YYYY-MM-DD
	BudgetPesewas *int
	Note          string
}

type Request struct {
	ID                   string
	OrganizerUserID      string
	OrganizerDisplayName string
	StatusCode           string
	StatusName           string
	EventTypeCode        *string
	EventTypeName        *string
	GenreCode            *string
	GenreName            *string
	PlaceCode            *string
	PlaceName            *string
	EventDate            *string
	BudgetPesewas        *int
	CurrencyCode         string
	Note                 string
	CreatedAt            time.Time
}

// Create records a new open request for an organizer. It needs *something* to
// go on: at least one of genre, event type, region, date, budget or a note.
func (s *Service) Create(ctx context.Context, organizerUserID string, in Input) (*Request, error) {
	oid, err := parseUUID(organizerUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid caller id", ErrInvalidInput)
	}
	note := strings.TrimSpace(in.Note)
	if len(note) > maxNoteLen {
		return nil, fmt.Errorf("%w: note is too long (max %d characters)", ErrInvalidInput, maxNoteLen)
	}
	params := db.InsertTalentRequestParams{
		OrganizerUserID: oid, EventTypeCode: blankNil(in.EventTypeCode), GenreCode: blankNil(in.GenreCode),
		PlaceCode: blankNil(in.PlaceCode), Note: note,
	}
	if d := blankNil(in.EventDate); d != nil {
		t, err := time.Parse("2006-01-02", *d)
		if err != nil {
			return nil, fmt.Errorf("%w: date must look like 2026-12-31", ErrInvalidInput)
		}
		if t.Before(time.Now().UTC().Truncate(24 * time.Hour)) {
			return nil, fmt.Errorf("%w: the event date is in the past", ErrInvalidInput)
		}
		params.EventDate = pgtype.Date{Time: t, Valid: true}
	}
	if in.BudgetPesewas != nil {
		b := *in.BudgetPesewas
		if b < 1 || b > maxBudgetPesewas {
			return nil, fmt.Errorf("%w: budget must be between 0.01 and 1,000,000 GHS", ErrInvalidInput)
		}
		_ = params.BudgetAmount.Scan(fmt.Sprintf("%d.%02d", b/100, b%100))
	}
	if params.EventTypeCode == nil && params.GenreCode == nil && params.PlaceCode == nil &&
		!params.EventDate.Valid && !params.BudgetAmount.Valid && note == "" {
		return nil, fmt.Errorf("%w: tell us at least a little about what you need", ErrInvalidInput)
	}

	var out *Request
	err = s.withTx(ctx, func(q *db.Queries) error {
		id, err := q.InsertTalentRequest(ctx, params)
		if err != nil {
			return fmt.Errorf("insert request: %w", err)
		}
		rows, err := q.ListTalentRequests(ctx, db.ListTalentRequestsParams{ID: id})
		if err != nil || len(rows) == 0 {
			return fmt.Errorf("reload request: %w", err)
		}
		out = toRequest(rows[0])
		// An unknown lookup code resolves to NULL; reject it (the tx rolls back).
		if (params.EventTypeCode != nil && out.EventTypeCode == nil) ||
			(params.GenreCode != nil && out.GenreCode == nil) ||
			(params.PlaceCode != nil && out.PlaceCode == nil) {
			return fmt.Errorf("%w: unknown event type, genre or region", ErrInvalidInput)
		}
		return nil
	})
	return out, err
}

// ListMine returns the organizer's own requests, newest first.
func (s *Service) ListMine(ctx context.Context, organizerUserID string) ([]Request, error) {
	oid, err := parseUUID(organizerUserID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid caller id", ErrInvalidInput)
	}
	return s.list(ctx, db.ListTalentRequestsParams{OrganizerUserID: oid})
}

// ListForAdmin returns every request, optionally filtered to one status.
func (s *Service) ListForAdmin(ctx context.Context, statusCode *string) ([]Request, error) {
	return s.list(ctx, db.ListTalentRequestsParams{StatusCode: blankNil(statusCode)})
}

func (s *Service) list(ctx context.Context, p db.ListTalentRequestsParams) ([]Request, error) {
	var out []Request
	err := s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListTalentRequests(ctx, p)
		if err != nil {
			return fmt.Errorf("list requests: %w", err)
		}
		out = make([]Request, 0, len(rows))
		for _, r := range rows {
			out = append(out, *toRequest(r))
		}
		return nil
	})
	return out, err
}

// SetStatus moves a request to open, matched, or closed. Admin-only.
func (s *Service) SetStatus(ctx context.Context, id, statusCode string) (*Request, error) {
	rid, err := parseUUID(id)
	if err != nil {
		return nil, ErrNotFound
	}
	if !adminStatuses[statusCode] {
		return nil, fmt.Errorf("%w: status must be open, matched, or closed", ErrInvalidInput)
	}
	var out *Request
	err = s.withTx(ctx, func(q *db.Queries) error {
		n, err := q.SetTalentRequestStatus(ctx, db.SetTalentRequestStatusParams{ID: rid, StatusCode: statusCode})
		if err != nil {
			return fmt.Errorf("set status: %w", err)
		}
		if n == 0 {
			return ErrNotFound
		}
		rows, err := q.ListTalentRequests(ctx, db.ListTalentRequestsParams{ID: rid})
		if err != nil || len(rows) == 0 {
			return fmt.Errorf("reload request: %w", err)
		}
		out = toRequest(rows[0])
		return nil
	})
	return out, err
}

func toRequest(r db.ListTalentRequestsRow) *Request {
	out := &Request{
		ID: uuidString(r.ID), OrganizerUserID: uuidString(r.OrganizerUserID), OrganizerDisplayName: r.OrganizerDisplayName,
		StatusCode: r.StatusCode, StatusName: r.StatusName,
		EventTypeCode: r.EventTypeCode, EventTypeName: r.EventTypeName,
		GenreCode: r.GenreCode, GenreName: r.GenreName, PlaceCode: r.PlaceCode, PlaceName: r.PlaceName,
		CurrencyCode: r.CurrencyCode, Note: r.Note, CreatedAt: r.CreatedAt.Time,
	}
	if r.EventDate.Valid {
		d := r.EventDate.Time.Format("2006-01-02")
		out.EventDate = &d
	}
	if r.BudgetPesewas > 0 {
		b := int(r.BudgetPesewas)
		out.BudgetPesewas = &b
	}
	return out
}

func blankNil(s *string) *string {
	if s == nil || strings.TrimSpace(*s) == "" {
		return nil
	}
	t := strings.TrimSpace(*s)
	return &t
}

func parseUUID(s string) (pgtype.UUID, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("invalid id %q: %w", s, err)
	}
	return pgtype.UUID{Bytes: id, Valid: true}, nil
}

func uuidString(u pgtype.UUID) string { return uuid.UUID(u.Bytes).String() }
