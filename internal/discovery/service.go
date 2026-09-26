// Package discovery is the read side of the marketplace: searching and viewing
// talent. It never exposes contact details; those unlock at booking `agreed`.
package discovery

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	defaultPageSize = 20
	maxPageSize     = 50
	defaultCountry  = "GH"
)

var (
	ErrNotFound      = errors.New("talent not found")
	ErrInvalidCursor = errors.New("invalid cursor")
)

type Service struct {
	q *db.Queries
}

func NewService(q *db.Queries) *Service {
	return &Service{q: q}
}

// Filter narrows a search. Codes are immutable lookup codes. Values within one
// field are OR-ed; different fields are AND-ed. PlaceCode matches a place and
// everything beneath it (region -> district -> city) via home or service areas.
type Filter struct {
	GenreCodes    []string
	TypeCodes     []string
	LanguageCodes []string
	PlaceCode     string
}

type Tag struct {
	Code string
	Name string
}

type Rate struct {
	Amount        string // decimal string, e.g. "150.00"
	CurrencyCode  string
	RateUnitCode  string
	EventTypeCode *string
}

type Card struct {
	ID            string
	DisplayName   string
	Headline      *string
	HomePlaceName *string
	FromRate      *Rate // cheapest current rate, nil if none
	Genres        []Tag
	Types         []Tag
	createdAt     time.Time
}

type Profile struct {
	Card
	Bio          *string
	Languages    []Tag
	ServiceAreas []Tag
	Rates        []Rate
}

type Page struct {
	Items      []Card
	NextCursor string // empty when there are no more results
}

// Search returns searchable, active talent newest first using keyset pagination.
func (s *Service) Search(ctx context.Context, f Filter, first int, after string) (Page, error) {
	size := first
	if size <= 0 {
		size = defaultPageSize
	}
	if size > maxPageSize {
		size = maxPageSize
	}

	params := db.SearchTalentParams{
		GenreCodes:    nonNil(f.GenreCodes),
		TypeCodes:     nonNil(f.TypeCodes),
		LanguageCodes: nonNil(f.LanguageCodes),
		PlaceCode:     f.PlaceCode,
		CountryCode:   defaultCountry,
		PageSize:      int32(size + 1), // one extra row tells us whether another page exists
	}
	if after != "" {
		at, id, err := decodeCursor(after)
		if err != nil {
			return Page{}, err
		}
		params.CursorCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.CursorID = pgtype.UUID{Bytes: id, Valid: true}
	}

	rows, err := s.q.SearchTalent(ctx, params)
	if err != nil {
		return Page{}, fmt.Errorf("search talent: %w", err)
	}
	hasMore := len(rows) > size
	if hasMore {
		rows = rows[:size]
	}

	cards := make([]Card, 0, len(rows))
	idList := make([]pgtype.UUID, 0, len(rows))
	for _, r := range rows {
		cards = append(cards, Card{
			ID:            uuidString(r.ID),
			DisplayName:   r.DisplayName,
			Headline:      r.Headline,
			HomePlaceName: r.HomePlaceName,
			createdAt:     r.CreatedAt.Time,
		})
		idList = append(idList, r.ID)
	}
	if err := s.attachTags(ctx, cards, idList); err != nil {
		return Page{}, err
	}

	page := Page{Items: cards}
	if hasMore && len(cards) > 0 {
		last := cards[len(cards)-1]
		page.NextCursor = encodeCursor(last.createdAt, last.ID)
	}
	return page, nil
}

// Get returns the full public profile of a searchable, active talent.
func (s *Service) Get(ctx context.Context, id string) (*Profile, error) {
	parsed, err := uuid.Parse(id)
	if err != nil {
		return nil, ErrNotFound
	}
	pgID := pgtype.UUID{Bytes: parsed, Valid: true}
	row, err := s.q.GetSearchableTalent(ctx, pgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get talent: %w", err)
	}

	p := &Profile{
		Card: Card{
			ID:            uuidString(row.ID),
			DisplayName:   row.DisplayName,
			Headline:      row.Headline,
			HomePlaceName: row.HomePlaceName,
			createdAt:     row.CreatedAt.Time,
		},
		Bio: row.Bio,
	}
	ids := []pgtype.UUID{pgID}
	cards := []Card{p.Card}
	if err := s.attachTags(ctx, cards, ids); err != nil {
		return nil, err
	}
	p.Card = cards[0]

	langs, err := s.q.ListLanguagesForTalent(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list languages: %w", err)
	}
	for _, l := range langs {
		p.Languages = append(p.Languages, Tag{Code: l.Code, Name: l.Name})
	}
	areas, err := s.q.ListServiceAreasForTalent(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list service areas: %w", err)
	}
	for _, a := range areas {
		p.ServiceAreas = append(p.ServiceAreas, Tag{Code: a.Code, Name: a.Name})
	}
	rates, err := s.q.ListCurrentRatesForTalent(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list rates: %w", err)
	}
	for _, r := range rates {
		p.Rates = append(p.Rates, rateFrom(r.Amount, r.CurrencyCode, r.RateUnitCode, r.EventTypeCode))
	}
	return p, nil
}

// attachTags fills genres, types and the cheapest current rate on cards with
// three batched queries (no per-card queries).
func (s *Service) attachTags(ctx context.Context, cards []Card, ids []pgtype.UUID) error {
	if len(cards) == 0 {
		return nil
	}
	index := make(map[string]int, len(cards))
	for i, c := range cards {
		index[c.ID] = i
	}

	genres, err := s.q.ListGenresForTalent(ctx, ids)
	if err != nil {
		return fmt.Errorf("list genres: %w", err)
	}
	for _, g := range genres {
		i := index[uuidString(g.TalentProfileID)]
		cards[i].Genres = append(cards[i].Genres, Tag{Code: g.Code, Name: g.Name})
	}
	types, err := s.q.ListTypesForTalent(ctx, ids)
	if err != nil {
		return fmt.Errorf("list types: %w", err)
	}
	for _, t := range types {
		i := index[uuidString(t.TalentProfileID)]
		cards[i].Types = append(cards[i].Types, Tag{Code: t.Code, Name: t.Name})
	}
	rates, err := s.q.ListCurrentRatesForTalent(ctx, ids) // ordered cheapest first
	if err != nil {
		return fmt.Errorf("list rates: %w", err)
	}
	for _, r := range rates {
		i := index[uuidString(r.TalentProfileID)]
		if cards[i].FromRate == nil {
			rate := rateFrom(r.Amount, r.CurrencyCode, r.RateUnitCode, r.EventTypeCode)
			cards[i].FromRate = &rate
		}
	}
	return nil
}

func rateFrom(amount, currency, unit string, eventType *string) Rate {
	return Rate{Amount: amount, CurrencyCode: currency, RateUnitCode: unit, EventTypeCode: eventType}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func uuidString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}

// Cursors are opaque to clients: base64url("<unix micros>|<uuid>").
func encodeCursor(at time.Time, id string) string {
	raw := strconv.FormatInt(at.UnixMicro(), 10) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, uuid.UUID, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	micros, idStr, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	n, err := strconv.ParseInt(micros, 10, 64)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrInvalidCursor
	}
	return time.UnixMicro(n).UTC(), id, nil
}
