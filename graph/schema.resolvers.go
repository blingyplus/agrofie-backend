package graph

import (
	"context"

	"github.com/blingyplus/agrofie-backend/graph/model"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
)

// UpdateLookupName is the resolver for the updateLookupName field.
func (r *mutationResolver) UpdateLookupName(ctx context.Context, table model.LookupTable, code string, name string) (*model.Lookup, error) {
	item, err := r.Lookups.UpdateName(ctx, lookup.Table(table), code, name)
	if err != nil {
		return nil, err
	}
	return toLookup(item), nil
}

// SetLookupActive is the resolver for the setLookupActive field.
func (r *mutationResolver) SetLookupActive(ctx context.Context, table model.LookupTable, code string, isActive bool) (*model.Lookup, error) {
	item, err := r.Lookups.SetActive(ctx, lookup.Table(table), code, isActive)
	if err != nil {
		return nil, err
	}
	return toLookup(item), nil
}

// Health is the resolver for the health field.
func (r *queryResolver) Health(ctx context.Context) (*model.Health, error) {
	auth, booking, payments := r.Prober.Probe(ctx)
	status := "ok"
	if auth != "ok" || booking != "ok" || payments != "ok" {
		status = "degraded"
	}
	return &model.Health{
		Status:   status,
		Service:  "gateway",
		Auth:     auth,
		Booking:  booking,
		Payments: payments,
	}, nil
}

// Countries is the resolver for the countries field.
func (r *queryResolver) Countries(ctx context.Context, activeOnly *bool) ([]*model.Country, error) {
	rows, err := r.Lookups.Countries(ctx, boolOr(activeOnly, true))
	if err != nil {
		return nil, err
	}
	out := make([]*model.Country, 0, len(rows))
	for _, row := range rows {
		out = append(out, &model.Country{
			ID:       row.ID,
			Code:     row.Code,
			Name:     row.Name,
			IsActive: row.IsActive,
		})
	}
	return out, nil
}

// GeoPlaces is the resolver for the geoPlaces field.
func (r *queryResolver) GeoPlaces(ctx context.Context, countryCode *string, parentCode *string, activeOnly *bool) ([]*model.GeoPlace, error) {
	cc := "GH"
	if countryCode != nil && *countryCode != "" {
		cc = *countryCode
	}
	rows, err := r.Lookups.GeoPlaces(ctx, cc, parentCode, boolOr(activeOnly, true))
	if err != nil {
		return nil, err
	}
	out := make([]*model.GeoPlace, 0, len(rows))
	for _, row := range rows {
		out = append(out, &model.GeoPlace{
			ID:         row.ID,
			Code:       row.Code,
			Name:       row.Name,
			PlaceLevel: row.PlaceLevel,
			ParentCode: row.ParentCode,
			IsActive:   row.IsActive,
		})
	}
	return out, nil
}

// Genres is the resolver for the genres field.
func (r *queryResolver) Genres(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.Genres)
}

// TalentTypes is the resolver for the talentTypes field.
func (r *queryResolver) TalentTypes(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.TalentTypes)
}

// EventTypes is the resolver for the eventTypes field.
func (r *queryResolver) EventTypes(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.EventTypes)
}

// Languages is the resolver for the languages field.
func (r *queryResolver) Languages(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.Languages)
}

// Roles is the resolver for the roles field.
func (r *queryResolver) Roles(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.Roles)
}

// BookingStatuses is the resolver for the bookingStatuses field.
func (r *queryResolver) BookingStatuses(ctx context.Context, activeOnly *bool) ([]*model.Lookup, error) {
	return listLookups(ctx, boolOr(activeOnly, true), r.Lookups.BookingStatuses)
}

// Mutation returns MutationResolver implementation.
func (r *Resolver) Mutation() MutationResolver { return &mutationResolver{r} }

// Query returns QueryResolver implementation.
func (r *Resolver) Query() QueryResolver { return &queryResolver{r} }

type (
	mutationResolver struct{ *Resolver }
	queryResolver    struct{ *Resolver }
)

func boolOr(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func toLookup(item *lookup.Item) *model.Lookup {
	return &model.Lookup{
		ID:        item.ID,
		Code:      item.Code,
		Name:      item.Name,
		SortOrder: int(item.SortOrder),
		IsActive:  item.IsActive,
	}
}

func listLookups(ctx context.Context, activeOnly bool, fn func(context.Context, bool) ([]lookup.Item, error)) ([]*model.Lookup, error) {
	rows, err := fn(ctx, activeOnly)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Lookup, 0, len(rows))
	for i := range rows {
		out = append(out, toLookup(&rows[i]))
	}
	return out, nil
}
