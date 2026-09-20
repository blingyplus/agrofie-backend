package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Resolver struct {
	DB         *pgxpool.Pool
	AuthURL    string
	BookingURL string
	PaymentsURL string
	HTTPClient *http.Client
}

func NewResolver(db *pgxpool.Pool, authURL, bookingURL, paymentsURL string) *Resolver {
	return &Resolver{
		DB:          db,
		AuthURL:     authURL,
		BookingURL:  bookingURL,
		PaymentsURL: paymentsURL,
		HTTPClient:  &http.Client{Timeout: 3 * time.Second},
	}
}

func (r *Resolver) probe(ctx context.Context, base string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/healthz", nil)
	if err != nil {
		return "unreachable"
	}
	res, err := r.HTTPClient.Do(req)
	if err != nil {
		return "unreachable"
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	if res.StatusCode == http.StatusOK {
		return "ok"
	}
	return fmt.Sprintf("status_%d", res.StatusCode)
}

type Health struct {
	Status   string `json:"status"`
	Service  string `json:"service"`
	Auth     string `json:"auth"`
	Booking  string `json:"booking"`
	Payments string `json:"payments"`
}

type Lookup struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	IsActive  bool   `json:"isActive"`
}

type Country struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	IsActive bool   `json:"isActive"`
}

type GeoPlace struct {
	ID         string  `json:"id"`
	Code       string  `json:"code"`
	Name       string  `json:"name"`
	PlaceLevel string  `json:"placeLevel"`
	ParentCode *string `json:"parentCode"`
	IsActive   bool    `json:"isActive"`
}

func (r *Resolver) Health(ctx context.Context) Health {
	auth := r.probe(ctx, r.AuthURL)
	booking := r.probe(ctx, r.BookingURL)
	payments := r.probe(ctx, r.PaymentsURL)
	status := "ok"
	if auth != "ok" || booking != "ok" || payments != "ok" {
		status = "degraded"
	}
	return Health{
		Status:   status,
		Service:  "gateway",
		Auth:     auth,
		Booking:  booking,
		Payments: payments,
	}
}

var lookupTables = map[string]string{
	"GENRES":               "genres",
	"TALENT_TYPES":         "talent_types",
	"EVENT_TYPES":          "event_types",
	"LANGUAGES":            "languages",
	"ROLES":                "roles",
	"BOOKING_STATUSES":     "booking_statuses",
	"DISPUTE_REASONS":      "dispute_reasons",
	"CANCELLATION_REASONS": "cancellation_reasons",
}

func (r *Resolver) listLookup(ctx context.Context, table string, activeOnly bool) ([]Lookup, error) {
	q := fmt.Sprintf(`SELECT id::text, code, name, sort_order, is_active FROM %s`, table)
	if activeOnly {
		q += ` WHERE is_active = true`
	}
	q += ` ORDER BY sort_order, name`
	rows, err := r.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lookup
	for rows.Next() {
		var l Lookup
		if err := rows.Scan(&l.ID, &l.Code, &l.Name, &l.SortOrder, &l.IsActive); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (r *Resolver) Countries(ctx context.Context, activeOnly bool) ([]Country, error) {
	q := `SELECT id::text, code, name, is_active FROM countries`
	if activeOnly {
		q += ` WHERE is_active = true`
	}
	q += ` ORDER BY sort_order, name`
	rows, err := r.DB.Query(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Country
	for rows.Next() {
		var c Country
		if err := rows.Scan(&c.ID, &c.Code, &c.Name, &c.IsActive); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *Resolver) GeoPlaces(ctx context.Context, countryCode string, parentCode *string, activeOnly bool) ([]GeoPlace, error) {
	q := `
SELECT g.id::text, g.code, g.name, g.place_level, p.code, g.is_active
FROM geo_places g
JOIN countries c ON c.id = g.country_id
LEFT JOIN geo_places p ON p.id = g.parent_id
WHERE c.code = $1`
	args := []any{countryCode}
	argN := 2
	if parentCode == nil {
		q += ` AND g.parent_id IS NULL`
	} else {
		q += fmt.Sprintf(` AND p.code = $%d`, argN)
		args = append(args, *parentCode)
		argN++
	}
	if activeOnly {
		q += ` AND g.is_active = true`
	}
	q += ` ORDER BY g.sort_order, g.name`

	rows, err := r.DB.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GeoPlace
	for rows.Next() {
		var g GeoPlace
		var parent *string
		if err := rows.Scan(&g.ID, &g.Code, &g.Name, &g.PlaceLevel, &parent, &g.IsActive); err != nil {
			return nil, err
		}
		g.ParentCode = parent
		out = append(out, g)
	}
	return out, rows.Err()
}

func (r *Resolver) UpdateLookupName(ctx context.Context, tableKey, code, name string) (*Lookup, error) {
	table, ok := lookupTables[tableKey]
	if !ok {
		return nil, fmt.Errorf("unknown lookup table %s", tableKey)
	}
	q := fmt.Sprintf(`
UPDATE %s SET name = $1, updated_at = now()
WHERE code = $2
RETURNING id::text, code, name, sort_order, is_active`, table)
	var l Lookup
	err := r.DB.QueryRow(ctx, q, name, code).Scan(&l.ID, &l.Code, &l.Name, &l.SortOrder, &l.IsActive)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Resolver) SetLookupActive(ctx context.Context, tableKey, code string, isActive bool) (*Lookup, error) {
	table, ok := lookupTables[tableKey]
	if !ok {
		return nil, fmt.Errorf("unknown lookup table %s", tableKey)
	}
	q := fmt.Sprintf(`
UPDATE %s SET is_active = $1, updated_at = now()
WHERE code = $2
RETURNING id::text, code, name, sort_order, is_active`, table)
	var l Lookup
	err := r.DB.QueryRow(ctx, q, isActive, code).Scan(&l.ID, &l.Code, &l.Name, &l.SortOrder, &l.IsActive)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *Resolver) Genres(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "genres", activeOnly)
}
func (r *Resolver) TalentTypes(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "talent_types", activeOnly)
}
func (r *Resolver) EventTypes(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "event_types", activeOnly)
}
func (r *Resolver) Languages(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "languages", activeOnly)
}
func (r *Resolver) Roles(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "roles", activeOnly)
}
func (r *Resolver) BookingStatuses(ctx context.Context, activeOnly bool) ([]Lookup, error) {
	return r.listLookup(ctx, "booking_statuses", activeOnly)
}
