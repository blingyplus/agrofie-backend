// Package talentprofile lets a talent manage their own profile, rates and
// payout account. Every method resolves the caller's talent_profile_id from
// their user ID server-side; nothing here trusts a client-supplied profile ID.
package talentprofile

import (
	"context"
	"errors"
	"fmt"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/discovery"
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrNotTalent       = errors.New("caller has no talent profile")
	ErrUnknownCode     = errors.New("unknown lookup code")
	ErrInvalidInput    = errors.New("invalid input")
	ErrNoPayoutAccount = errors.New("no payout account connected")
)

// beginner is satisfied by both *pgxpool.Pool (a real transaction) and pgx.Tx
// (a pseudo-nested transaction / savepoint), so tests can pass an already-open
// test transaction and get the same atomicity guarantees as production.
type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db                beginner
	payments          payments.Provider
	commissionPercent float64 // Agrofie's share, e.g. 12.5. Talent bears the Paystack fee.
}

func NewService(conn beginner, provider payments.Provider, commissionPercent float64) *Service {
	return &Service{db: conn, payments: provider, commissionPercent: commissionPercent}
}

// withTx runs fn in its own transaction (or savepoint, if db is already a
// pgx.Tx) so a mid-way failure — such as an unknown lookup code — leaves
// nothing changed.
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

type UpdateBasicsInput struct {
	Headline      *string
	Bio           *string
	HomePlaceCode *string // geo_places code; nil leaves it unchanged... except see note below
	GenreCodes    []string
	TypeCodes     []string
	LanguageCodes []string
	ServiceAreas  []string // geo_places codes the talent will travel to
}

// UpdateBasics replaces the talent's headline, bio, home place, and every tag
// list given. A nil slice leaves that tag list unchanged; pass an empty slice
// to clear it. HomePlaceCode, if non-nil, must resolve to a real geo place.
func (s *Service) UpdateBasics(ctx context.Context, userID string, in UpdateBasicsInput) error {
	return s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}

		var homePlaceID pgtype.UUID
		if in.HomePlaceCode != nil {
			id, err := q.GeoPlaceIDByCode(ctx, *in.HomePlaceCode)
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: home place %q", ErrUnknownCode, *in.HomePlaceCode)
			}
			if err != nil {
				return fmt.Errorf("look up home place: %w", err)
			}
			homePlaceID = id
		}

		if err := q.UpdateTalentProfileBasics(ctx, db.UpdateTalentProfileBasicsParams{
			ID: profileID, Headline: in.Headline, Bio: in.Bio, HomeGeoPlaceID: homePlaceID,
		}); err != nil {
			return fmt.Errorf("update basics: %w", err)
		}

		if in.GenreCodes != nil {
			if err := replaceTags(ctx, "genre", in.GenreCodes,
				func() error { return q.ClearTalentGenres(ctx, profileID) },
				func(code string) (int64, error) {
					return q.AddTalentGenre(ctx, db.AddTalentGenreParams{TalentProfileID: profileID, Code: code})
				}); err != nil {
				return err
			}
		}
		if in.TypeCodes != nil {
			if err := replaceTags(ctx, "talent type", in.TypeCodes,
				func() error { return q.ClearTalentTypes(ctx, profileID) },
				func(code string) (int64, error) {
					return q.AddTalentType(ctx, db.AddTalentTypeParams{TalentProfileID: profileID, Code: code})
				}); err != nil {
				return err
			}
		}
		if in.LanguageCodes != nil {
			if err := replaceTags(ctx, "language", in.LanguageCodes,
				func() error { return q.ClearTalentLanguages(ctx, profileID) },
				func(code string) (int64, error) {
					return q.AddTalentLanguage(ctx, db.AddTalentLanguageParams{TalentProfileID: profileID, Code: code})
				}); err != nil {
				return err
			}
		}
		if in.ServiceAreas != nil {
			if err := replaceTags(ctx, "service area", in.ServiceAreas,
				func() error { return q.ClearTalentServiceAreas(ctx, profileID) },
				func(code string) (int64, error) {
					return q.AddTalentServiceArea(ctx, db.AddTalentServiceAreaParams{TalentProfileID: profileID, Code: code})
				}); err != nil {
				return err
			}
		}
		return nil
	})
}

// replaceTags clears then re-inserts a tag list, failing with ErrUnknownCode
// (and leaving nothing changed, since callers run inside a transaction) if
// any code does not resolve to a lookup row.
func replaceTags(ctx context.Context, kind string, codes []string, clear func() error, add func(code string) (int64, error)) error {
	if err := clear(); err != nil {
		return fmt.Errorf("clear %ss: %w", kind, err)
	}
	for _, code := range codes {
		rows, err := add(code)
		if err != nil {
			return fmt.Errorf("add %s %q: %w", kind, code, err)
		}
		if rows != 1 {
			return fmt.Errorf("%w: %s %q", ErrUnknownCode, kind, code)
		}
	}
	return nil
}

type RateInput struct {
	Amount        string // decimal string, e.g. "500.00"
	CurrencyCode  string
	RateUnitCode  string
	EventTypeCode *string // nil = applies to any event type
}

// SetRates replaces all of the talent's current rates. Existing rates are
// closed (effective_to = now), never deleted, so pricing history is kept.
func (s *Service) SetRates(ctx context.Context, userID string, rates []RateInput) error {
	return s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		if err := q.CloseCurrentTalentRates(ctx, profileID); err != nil {
			return fmt.Errorf("close current rates: %w", err)
		}
		for _, r := range rates {
			rows, err := q.InsertTalentRate(ctx, db.InsertTalentRateParams{
				TalentProfileID: profileID,
				Column2:         mustNumeric(r.Amount),
				CurrencyCode:    r.CurrencyCode,
				RateUnitCode:    r.RateUnitCode,
				EventTypeCode:   r.EventTypeCode,
			})
			if err != nil {
				return fmt.Errorf("insert rate: %w", err)
			}
			if rows != 1 {
				return fmt.Errorf("%w: rate unit %q, currency %q, or event type %q",
					ErrUnknownCode, r.RateUnitCode, r.CurrencyCode, ptrString(r.EventTypeCode))
			}
		}
		return nil
	})
}

type ConnectPayoutInput struct {
	BusinessName   string
	SettlementType payments.SettlementType
	BankCode       string
	AccountNumber  string
}

type PayoutAccount struct {
	SettlementType     string
	BankName           string
	AccountNumberLast4 string
	AccountName        string
}

// ConnectPayout creates (or updates) the talent's Paystack subaccount and
// stores a display-only record. The full account number is never persisted;
// Paystack is the source of truth for it.
func (s *Service) ConnectPayout(ctx context.Context, userID string, in ConnectPayoutInput) (*PayoutAccount, error) {
	if len(in.AccountNumber) < 4 {
		return nil, fmt.Errorf("%w: account number is too short", ErrInvalidInput)
	}

	// The Paystack call happens outside the DB transaction: it is not something
	// a rollback can undo, and holding a transaction open across a network call
	// would block other writes for no benefit. A retry after a storage failure
	// simply creates/updates the same subaccount again (see UpsertPayoutAccount).
	created, err := s.payments.CreateSubaccount(ctx, payments.SubaccountInput{
		BusinessName:   in.BusinessName,
		SettlementType: in.SettlementType,
		BankCode:       in.BankCode,
		AccountNumber:  in.AccountNumber,
	}, s.commissionPercent)
	if err != nil {
		return nil, fmt.Errorf("create payout subaccount: %w", err)
	}

	last4 := in.AccountNumber[len(in.AccountNumber)-4:]
	err = s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		_, err = q.UpsertPayoutAccount(ctx, db.UpsertPayoutAccountParams{
			TalentProfileID:        profileID,
			SettlementType:         string(in.SettlementType),
			BankCode:               in.BankCode,
			BankName:               created.BankName,
			AccountNumberLast4:     last4,
			AccountName:            created.AccountName,
			ProviderSubaccountCode: created.SubaccountCode,
		})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("store payout account: %w", err)
	}

	return &PayoutAccount{
		SettlementType:     string(in.SettlementType),
		BankName:           created.BankName,
		AccountNumberLast4: last4,
		AccountName:        created.AccountName,
	}, nil
}

// OwnProfile returns the caller's full talent profile, including unpublished
// changes: unlike discovery.Service.Get, there is no is_searchable gate, since
// a talent must be able to see and edit their profile before verification.
func (s *Service) OwnProfile(ctx context.Context, userID string) (*discovery.Profile, error) {
	var out *discovery.Profile
	err := s.withTx(ctx, func(q *db.Queries) error {
		uid, err := uuid.Parse(userID)
		if err != nil {
			return fmt.Errorf("%w: invalid user id", ErrInvalidInput)
		}
		row, err := q.GetOwnTalentProfileBasics(ctx, pgtype.UUID{Bytes: uid, Valid: true})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotTalent
		}
		if err != nil {
			return fmt.Errorf("get own profile: %w", err)
		}

		ids := []pgtype.UUID{row.ID}
		p := &discovery.Profile{Card: discovery.Card{
			ID: uuidString(row.ID), DisplayName: row.DisplayName, Headline: row.Headline, HomePlaceName: row.HomePlaceName,
		}, Bio: row.Bio}

		genres, err := q.ListGenresForTalent(ctx, ids)
		if err != nil {
			return fmt.Errorf("list genres: %w", err)
		}
		for _, g := range genres {
			p.Genres = append(p.Genres, discovery.Tag{Code: g.Code, Name: g.Name})
		}
		types, err := q.ListTypesForTalent(ctx, ids)
		if err != nil {
			return fmt.Errorf("list types: %w", err)
		}
		for _, t := range types {
			p.Types = append(p.Types, discovery.Tag{Code: t.Code, Name: t.Name})
		}
		langs, err := q.ListLanguagesForTalent(ctx, ids)
		if err != nil {
			return fmt.Errorf("list languages: %w", err)
		}
		for _, l := range langs {
			p.Languages = append(p.Languages, discovery.Tag{Code: l.Code, Name: l.Name})
		}
		areas, err := q.ListServiceAreasForTalent(ctx, ids)
		if err != nil {
			return fmt.Errorf("list service areas: %w", err)
		}
		for _, a := range areas {
			p.ServiceAreas = append(p.ServiceAreas, discovery.Tag{Code: a.Code, Name: a.Name})
		}
		rates, err := q.ListCurrentRatesForTalent(ctx, ids)
		if err != nil {
			return fmt.Errorf("list rates: %w", err)
		}
		for _, r := range rates {
			p.Rates = append(p.Rates, discovery.Rate{Amount: r.Amount, CurrencyCode: r.CurrencyCode, RateUnitCode: r.RateUnitCode, EventTypeCode: r.EventTypeCode})
		}

		out = p
		return nil
	})
	return out, err
}

// GetMyPayoutAccount returns the caller's active payout account, or
// ErrNoPayoutAccount if none is connected yet.
func (s *Service) GetMyPayoutAccount(ctx context.Context, userID string) (*PayoutAccount, error) {
	var out *PayoutAccount
	err := s.withTx(ctx, func(q *db.Queries) error {
		profileID, err := profileIDForUser(ctx, q, userID)
		if err != nil {
			return err
		}
		row, err := q.GetPayoutAccountByTalentProfileID(ctx, profileID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPayoutAccount
		}
		if err != nil {
			return fmt.Errorf("get payout account: %w", err)
		}
		out = &PayoutAccount{
			SettlementType:     row.SettlementType,
			BankName:           row.BankName,
			AccountNumberLast4: row.AccountNumberLast4,
			AccountName:        row.AccountName,
		}
		return nil
	})
	return out, err
}

// ListSettlementBanks lists the banks or mobile money telcos a talent can
// pick from when connecting a payout account.
func (s *Service) ListSettlementBanks(ctx context.Context, t payments.SettlementType) ([]payments.Bank, error) {
	return s.payments.ListSettlementBanks(ctx, "GHS", t)
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

func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func uuidString(u pgtype.UUID) string {
	return uuid.UUID(u.Bytes).String()
}

func mustNumeric(decimal string) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(decimal)
	return n
}
