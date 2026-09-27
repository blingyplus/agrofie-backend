// Package verification is the admin verification queue: talent submit
// evidence, admins approve or reject it. Approving an `identity`
// verification is what makes a talent searchable (see DOMAIN.md); other
// verification types are recorded but do not affect search visibility yet.
package verification

import (
	"context"
	"errors"
	"fmt"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrUnknownCode = errors.New("unknown lookup code")
	ErrNotFound    = errors.New("verification not found")
	ErrNotPending  = errors.New("verification already reviewed")
)

// identityTypeCode is the one verification type that gates search
// visibility today. See DOMAIN.md: "Talent searchable only if verification
// allows and is_searchable."
const identityTypeCode = "identity"

// beginner is satisfied by both *pgxpool.Pool and pgx.Tx (a pseudo-nested
// transaction), matching internal/talentprofile's pattern.
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

type SubmitInput struct {
	TypeCode    string
	EvidenceRef *string
	Notes       *string
}

type Verification struct {
	ID          string
	TypeCode    string
	TypeName    string
	StatusCode  string
	StatusName  string
	EvidenceRef *string
	Notes       *string
	ReviewNotes *string
}

type PendingItem struct {
	ID                string
	TalentUserID      string
	TalentDisplayName string
	TypeCode          string
	TypeName          string
	EvidenceRef       *string
	Notes             *string
}

// Submit records a new pending verification request for userID.
func (s *Service) Submit(ctx context.Context, userID string, in SubmitInput) (*Verification, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	var out *Verification
	err = s.withTx(ctx, func(q *db.Queries) error {
		id, err := q.InsertVerification(ctx, db.InsertVerificationParams{
			UserID: uid, Code: in.TypeCode, EvidenceRef: in.EvidenceRef, Notes: in.Notes,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: verification type %q", ErrUnknownCode, in.TypeCode)
		}
		if err != nil {
			return fmt.Errorf("insert verification: %w", err)
		}
		row, err := q.GetVerificationByID(ctx, id)
		if err != nil {
			return fmt.Errorf("read back verification: %w", err)
		}
		out = fromGetRow(row)
		return nil
	})
	return out, err
}

// ListPending lists every verification awaiting review, oldest first (so the
// admin queue works first-in-first-out).
func (s *Service) ListPending(ctx context.Context) ([]PendingItem, error) {
	var out []PendingItem
	err := s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListPendingVerifications(ctx)
		if err != nil {
			return fmt.Errorf("list pending: %w", err)
		}
		out = make([]PendingItem, 0, len(rows))
		for _, r := range rows {
			out = append(out, PendingItem{
				ID: uuidString(r.ID), TalentUserID: uuidString(r.UserID), TalentDisplayName: r.TalentDisplayName,
				TypeCode: r.TypeCode, TypeName: r.TypeName, EvidenceRef: r.EvidenceRef, Notes: r.Notes,
			})
		}
		return nil
	})
	return out, err
}

// ListMine returns the caller's own verification history, newest first.
func (s *Service) ListMine(ctx context.Context, userID string) ([]Verification, error) {
	uid, err := parseUUID(userID)
	if err != nil {
		return nil, err
	}
	var out []Verification
	err = s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.ListMyVerifications(ctx, uid)
		if err != nil {
			return fmt.Errorf("list mine: %w", err)
		}
		out = make([]Verification, 0, len(rows))
		for _, r := range rows {
			out = append(out, Verification{
				ID: uuidString(r.ID), TypeCode: r.TypeCode, TypeName: r.TypeName,
				StatusCode: r.StatusCode, StatusName: r.StatusName,
				EvidenceRef: r.EvidenceRef, Notes: r.Notes, ReviewNotes: r.ReviewNotes,
			})
		}
		return nil
	})
	return out, err
}

// Approve marks a pending verification verified. Approving an `identity`
// verification also makes the talent searchable.
func (s *Service) Approve(ctx context.Context, adminUserID, verificationID string, reviewNotes *string) (*Verification, error) {
	return s.review(ctx, adminUserID, verificationID, "verified", reviewNotes)
}

// Reject marks a pending verification rejected. It never changes
// searchability; an approved talent stays searchable until separately
// revoked (not built yet).
func (s *Service) Reject(ctx context.Context, adminUserID, verificationID string, reviewNotes *string) (*Verification, error) {
	return s.review(ctx, adminUserID, verificationID, "rejected", reviewNotes)
}

func (s *Service) review(ctx context.Context, adminUserID, verificationID, newStatus string, reviewNotes *string) (*Verification, error) {
	adminUID, err := parseUUID(adminUserID)
	if err != nil {
		return nil, err
	}
	vID, err := parseUUID(verificationID)
	if err != nil {
		return nil, ErrNotFound
	}

	var out *Verification
	err = s.withTx(ctx, func(q *db.Queries) error {
		before, err := q.GetVerificationForReview(ctx, vID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get verification: %w", err)
		}

		rows, err := q.ReviewVerification(ctx, db.ReviewVerificationParams{
			ID: vID, ReviewedBy: adminUID, ReviewNotes: reviewNotes, NewStatusCode: newStatus,
		})
		if err != nil {
			return fmt.Errorf("review verification: %w", err)
		}
		if rows != 1 {
			return ErrNotPending
		}

		if newStatus == "verified" && before.TypeCode == identityTypeCode {
			if _, err := q.SetTalentSearchableByUserID(ctx, db.SetTalentSearchableByUserIDParams{
				UserID: before.UserID, IsSearchable: true,
			}); err != nil {
				return fmt.Errorf("set searchable: %w", err)
			}
		}

		row, err := q.GetVerificationByID(ctx, vID)
		if err != nil {
			return fmt.Errorf("read back verification: %w", err)
		}
		out = fromGetRow(row)
		return nil
	})
	return out, err
}

func fromGetRow(r db.GetVerificationByIDRow) *Verification {
	return &Verification{
		ID: uuidString(r.ID), TypeCode: r.TypeCode, TypeName: r.TypeName,
		StatusCode: r.StatusCode, StatusName: r.StatusName,
		EvidenceRef: r.EvidenceRef, Notes: r.Notes, ReviewNotes: r.ReviewNotes,
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
