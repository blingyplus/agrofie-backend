// Package organizerprofile holds an organizer's own profile: display name,
// optional organisation and contact phone, home region (geo place) and the
// event types they host (lookup codes). Everything is scoped to the caller.
package organizerprofile

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput = errors.New("invalid input")
	ErrUnknownCode  = errors.New("unknown code")
)

const (
	maxNameLen = 120
	maxOrgLen  = 160
	maxEvents  = 20
)

var phoneRe = regexp.MustCompile(`^[0-9+()\- ]{6,24}$`)

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct{ db beginner }

func NewService(conn beginner) *Service { return &Service{db: conn} }

type Tag struct{ Code, Name string }

type Profile struct {
	DisplayName   string
	Organization  *string
	Phone         *string
	HomePlaceCode *string
	HomePlaceName *string
	EventTypes    []Tag
}

// UpdateInput: a nil field is unchanged. For the optional strings (organization,
// phone, home place) an empty string clears the value. EventTypeCodes replaces
// the whole set (empty slice clears it).
type UpdateInput struct {
	DisplayName    *string
	Organization   *string
	Phone          *string
	HomePlaceCode  *string
	EventTypeCodes []string
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

func parseUser(userID string) (pgtype.UUID, error) {
	u, err := uuid.Parse(userID)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%w: invalid user id", ErrInvalidInput)
	}
	return pgtype.UUID{Bytes: u, Valid: true}, nil
}

// Get returns the caller's profile (creating the empty row if an older account lacks one).
func (s *Service) Get(ctx context.Context, userID string) (*Profile, error) {
	uid, err := parseUser(userID)
	if err != nil {
		return nil, err
	}
	var out *Profile
	err = s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.EnsureOrganizerProfile(ctx, uid); err != nil {
			return fmt.Errorf("ensure profile: %w", err)
		}
		out, err = load(ctx, q, uid)
		return err
	})
	return out, err
}

func load(ctx context.Context, q *db.Queries, uid pgtype.UUID) (*Profile, error) {
	row, err := q.GetOrganizerProfile(ctx, uid)
	if err != nil {
		return nil, fmt.Errorf("load profile: %w", err)
	}
	types, err := q.ListOrganizerEventTypes(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("load event types: %w", err)
	}
	p := &Profile{
		DisplayName: row.DisplayName, Organization: row.Organization, Phone: row.Phone,
		HomePlaceCode: row.HomePlaceCode, HomePlaceName: row.HomePlaceName, EventTypes: []Tag{},
	}
	for _, t := range types {
		p.EventTypes = append(p.EventTypes, Tag{Code: t.Code, Name: t.Name})
	}
	return p, nil
}

func blankNil(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

// Update applies the changes atomically and returns the fresh profile.
func (s *Service) Update(ctx context.Context, userID string, in UpdateInput) (*Profile, error) {
	uid, err := parseUser(userID)
	if err != nil {
		return nil, err
	}
	if in.DisplayName != nil {
		n := strings.TrimSpace(*in.DisplayName)
		if n == "" || len(n) > maxNameLen {
			return nil, fmt.Errorf("%w: name is required (max %d characters)", ErrInvalidInput, maxNameLen)
		}
		in.DisplayName = &n
	}
	if o := blankNil(in.Organization); o != nil && len(*o) > maxOrgLen {
		return nil, fmt.Errorf("%w: organisation name is too long (max %d characters)", ErrInvalidInput, maxOrgLen)
	}
	if p := blankNil(in.Phone); p != nil && !phoneRe.MatchString(*p) {
		return nil, fmt.Errorf("%w: enter a valid phone number", ErrInvalidInput)
	}
	if len(in.EventTypeCodes) > maxEvents {
		return nil, fmt.Errorf("%w: too many event types", ErrInvalidInput)
	}

	var out *Profile
	err = s.withTx(ctx, func(q *db.Queries) error {
		pid, err := q.EnsureOrganizerProfile(ctx, uid)
		if err != nil {
			return fmt.Errorf("ensure profile: %w", err)
		}
		cur, err := q.GetOrganizerProfile(ctx, uid)
		if err != nil {
			return fmt.Errorf("load profile: %w", err)
		}
		org, phone := cur.Organization, cur.Phone
		if in.Organization != nil {
			org = blankNil(in.Organization)
		}
		if in.Phone != nil {
			phone = blankNil(in.Phone)
		}
		var place pgtype.UUID
		if in.HomePlaceCode != nil {
			if code := strings.TrimSpace(*in.HomePlaceCode); code != "" {
				place, err = q.GeoPlaceIDByCode(ctx, code)
				if errors.Is(err, pgx.ErrNoRows) {
					return fmt.Errorf("%w: region %q", ErrUnknownCode, code)
				}
				if err != nil {
					return fmt.Errorf("look up region: %w", err)
				}
			}
		} else if cur.HomePlaceCode != nil {
			place, err = q.GeoPlaceIDByCode(ctx, *cur.HomePlaceCode)
			if err != nil {
				return fmt.Errorf("keep region: %w", err)
			}
		}
		if err := q.UpdateOrganizerProfileBasics(ctx, db.UpdateOrganizerProfileBasicsParams{
			ID: pid, Organization: org, Phone: phone, HomeGeoPlaceID: place,
		}); err != nil {
			return fmt.Errorf("update profile: %w", err)
		}
		if in.DisplayName != nil {
			if err := q.UpdateProfileDisplayName(ctx, db.UpdateProfileDisplayNameParams{UserID: uid, DisplayName: *in.DisplayName}); err != nil {
				return fmt.Errorf("update name: %w", err)
			}
		}
		if in.EventTypeCodes != nil {
			if err := q.ClearOrganizerEventTypes(ctx, pid); err != nil {
				return fmt.Errorf("clear event types: %w", err)
			}
			seen := map[string]bool{}
			for _, c := range in.EventTypeCodes {
				if seen[c] {
					continue
				}
				seen[c] = true
				n, err := q.AddOrganizerEventType(ctx, db.AddOrganizerEventTypeParams{OrganizerProfileID: pid, Code: c})
				if err != nil {
					return fmt.Errorf("add event type: %w", err)
				}
				if n == 0 {
					return fmt.Errorf("%w: event type %q", ErrUnknownCode, c)
				}
			}
		}
		out, err = load(ctx, q, uid)
		return err
	})
	return out, err
}
