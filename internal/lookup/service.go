package lookup

import (
	"context"
	"fmt"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// Service is the application API for admin-controlled lookup tables.
// Domain rule: rename name freely; never change code.
type Service struct {
	q *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

type Item struct {
	ID        string
	Code      string
	Name      string
	SortOrder int32
	IsActive  bool
}

type Country struct {
	ID       string
	Code     string
	Name     string
	IsActive bool
}

type GeoPlace struct {
	ID         string
	Code       string
	Name       string
	PlaceLevel string
	ParentCode *string
	IsActive   bool
}

type Table string

const (
	TableGenres              Table = "GENRES"
	TableTalentTypes         Table = "TALENT_TYPES"
	TableEventTypes          Table = "EVENT_TYPES"
	TableLanguages           Table = "LANGUAGES"
	TableRoles               Table = "ROLES"
	TableBookingStatuses     Table = "BOOKING_STATUSES"
	TableDisputeReasons      Table = "DISPUTE_REASONS"
	TableCancellationReasons Table = "CANCELLATION_REASONS"
)

func (s *Service) Countries(ctx context.Context, activeOnly bool) ([]Country, error) {
	rows, err := s.q.ListCountries(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	out := make([]Country, 0, len(rows))
	for _, r := range rows {
		out = append(out, Country{ID: idString(r.ID), Code: r.Code, Name: r.Name, IsActive: r.IsActive})
	}
	return out, nil
}

func (s *Service) Genres(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListGenres(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListGenresRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) TalentTypes(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListTalentTypes(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListTalentTypesRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) EventTypes(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListEventTypes(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListEventTypesRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) Languages(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListLanguages(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListLanguagesRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) Roles(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListRoles(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListRolesRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) BookingStatuses(ctx context.Context, activeOnly bool) ([]Item, error) {
	rows, err := s.q.ListBookingStatuses(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	return itemsFrom(rows, func(r db.ListBookingStatusesRow) Item {
		return Item{ID: idString(r.ID), Code: r.Code, Name: r.Name, SortOrder: r.SortOrder, IsActive: r.IsActive}
	}), nil
}

func (s *Service) GeoPlaces(ctx context.Context, countryCode string, parentCode *string, activeOnly bool) ([]GeoPlace, error) {
	rows, err := s.q.ListGeoPlaces(ctx, db.ListGeoPlacesParams{
		CountryCode: countryCode,
		ParentCode:  parentCode,
		ActiveOnly:  activeOnly,
	})
	if err != nil {
		return nil, err
	}
	out := make([]GeoPlace, 0, len(rows))
	for _, r := range rows {
		out = append(out, GeoPlace{
			ID:         idString(r.ID),
			Code:       r.Code,
			Name:       r.Name,
			PlaceLevel: r.PlaceLevel,
			ParentCode: r.ParentCode,
			IsActive:   r.IsActive,
		})
	}
	return out, nil
}

func (s *Service) UpdateName(ctx context.Context, table Table, code, name string) (*Item, error) {
	switch table {
	case TableGenres:
		r, err := s.q.UpdateGenreName(ctx, db.UpdateGenreNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableTalentTypes:
		r, err := s.q.UpdateTalentTypeName(ctx, db.UpdateTalentTypeNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableEventTypes:
		r, err := s.q.UpdateEventTypeName(ctx, db.UpdateEventTypeNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableLanguages:
		r, err := s.q.UpdateLanguageName(ctx, db.UpdateLanguageNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableRoles:
		r, err := s.q.UpdateRoleName(ctx, db.UpdateRoleNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableBookingStatuses:
		r, err := s.q.UpdateBookingStatusName(ctx, db.UpdateBookingStatusNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableDisputeReasons:
		r, err := s.q.UpdateDisputeReasonName(ctx, db.UpdateDisputeReasonNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableCancellationReasons:
		r, err := s.q.UpdateCancellationReasonName(ctx, db.UpdateCancellationReasonNameParams{Name: name, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	default:
		return nil, fmt.Errorf("unknown lookup table %s", table)
	}
}

func (s *Service) SetActive(ctx context.Context, table Table, code string, isActive bool) (*Item, error) {
	switch table {
	case TableGenres:
		r, err := s.q.SetGenreActive(ctx, db.SetGenreActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableTalentTypes:
		r, err := s.q.SetTalentTypeActive(ctx, db.SetTalentTypeActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableEventTypes:
		r, err := s.q.SetEventTypeActive(ctx, db.SetEventTypeActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableLanguages:
		r, err := s.q.SetLanguageActive(ctx, db.SetLanguageActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableRoles:
		r, err := s.q.SetRoleActive(ctx, db.SetRoleActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableBookingStatuses:
		r, err := s.q.SetBookingStatusActive(ctx, db.SetBookingStatusActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableDisputeReasons:
		r, err := s.q.SetDisputeReasonActive(ctx, db.SetDisputeReasonActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	case TableCancellationReasons:
		r, err := s.q.SetCancellationReasonActive(ctx, db.SetCancellationReasonActiveParams{IsActive: isActive, Code: code})
		return itemPtr(r.ID, r.Code, r.Name, r.SortOrder, r.IsActive, err)
	default:
		return nil, fmt.Errorf("unknown lookup table %s", table)
	}
}

func itemsFrom[T any](rows []T, fn func(T) Item) []Item {
	out := make([]Item, 0, len(rows))
	for _, r := range rows {
		out = append(out, fn(r))
	}
	return out
}

func itemPtr(id pgtype.UUID, code, name string, sortOrder int32, isActive bool, err error) (*Item, error) {
	if err != nil {
		return nil, err
	}
	return &Item{ID: idString(id), Code: code, Name: name, SortOrder: sortOrder, IsActive: isActive}, nil
}

func idString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}
