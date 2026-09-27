package talentprofile_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/blingyplus/agrofie-backend/internal/talentprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like internal/discovery: real transaction, always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed talentprofile tests")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}

var seq int

func uniq(prefix string) string {
	seq++
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

// newTalentUser inserts a bare talent user + profile (no basics filled), and
// returns the userID.
func newTalentUser(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	email := uniq("t") + "@example.test"
	var userID string
	err := tx.QueryRow(context.Background(), `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT $1, id, gen_random_uuid() FROM user_statuses WHERE code = 'active'
		RETURNING id::text`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2)`, userID, "Test Talent"); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO talent_profiles (user_id) VALUES ($1)`, userID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	return userID
}

// newOrganizerUser inserts a user with no talent_profiles row.
func newOrganizerUser(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	email := uniq("o") + "@example.test"
	var userID string
	err := tx.QueryRow(context.Background(), `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT $1, id, gen_random_uuid() FROM user_statuses WHERE code = 'active'
		RETURNING id::text`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return userID
}

func newGenre(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	code := uniq("pgenre")
	if _, err := tx.Exec(context.Background(), `INSERT INTO genres (code, name) VALUES ($1, $1)`, code); err != nil {
		t.Fatalf("insert genre: %v", err)
	}
	return code
}

func newPlace(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	code := uniq("pplace")
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO geo_places (country_id, code, name, place_level)
		SELECT id, $1, $1, 'region' FROM countries WHERE code = 'GH'`, code); err != nil {
		t.Fatalf("insert place: %v", err)
	}
	return code
}

func newService(tx pgx.Tx, provider payments.Provider) *talentprofile.Service {
	return talentprofile.NewService(tx, provider, 12.5)
}

func TestUpdateBasicsSetsFieldsAndReplacesTags(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)
	genre := newGenre(t, tx)
	place := newPlace(t, tx)
	headline := "New headline"
	bio := "New bio"

	err := svc.UpdateBasics(context.Background(), userID, talentprofile.UpdateBasicsInput{
		Headline:      &headline,
		Bio:           &bio,
		HomePlaceCode: &place,
		GenreCodes:    []string{genre},
	})
	if err != nil {
		t.Fatal(err)
	}

	var gotHeadline, gotBio, gotPlaceCode string
	err = tx.QueryRow(context.Background(), `
		SELECT tp.headline, tp.bio, gp.code FROM talent_profiles tp
		JOIN geo_places gp ON gp.id = tp.home_geo_place_id
		WHERE tp.user_id = $1`, userID).Scan(&gotHeadline, &gotBio, &gotPlaceCode)
	if err != nil {
		t.Fatal(err)
	}
	if gotHeadline != headline || gotBio != bio || gotPlaceCode != place {
		t.Fatalf("got headline=%q bio=%q place=%q", gotHeadline, gotBio, gotPlaceCode)
	}

	var genreCount int
	tx.QueryRow(context.Background(), `
		SELECT count(*) FROM talent_genres tg JOIN talent_profiles tp ON tp.id = tg.talent_profile_id
		WHERE tp.user_id = $1`, userID).Scan(&genreCount)
	if genreCount != 1 {
		t.Fatalf("genre count = %d, want 1", genreCount)
	}
}

func TestUpdateBasicsRejectsUnknownGenreCodeAndChangesNothing(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)
	headline := "Should not stick"

	err := svc.UpdateBasics(context.Background(), userID, talentprofile.UpdateBasicsInput{
		Headline:   &headline,
		GenreCodes: []string{"not-a-real-genre-code"},
	})
	if !errors.Is(err, talentprofile.ErrUnknownCode) {
		t.Fatalf("got %v, want ErrUnknownCode", err)
	}

	var gotHeadline *string
	tx.QueryRow(context.Background(), `SELECT headline FROM talent_profiles WHERE user_id = $1`, userID).Scan(&gotHeadline)
	if gotHeadline != nil {
		t.Fatalf("headline = %v, want unchanged (nil)", gotHeadline)
	}
}

func TestUpdateBasicsRejectsUnknownHomePlaceCode(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)
	bad := "not-a-real-place"

	err := svc.UpdateBasics(context.Background(), userID, talentprofile.UpdateBasicsInput{HomePlaceCode: &bad})
	if !errors.Is(err, talentprofile.ErrUnknownCode) {
		t.Fatalf("got %v, want ErrUnknownCode", err)
	}
}

func TestUpdateBasicsRejectsCallerWithNoTalentProfile(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newOrganizerUser(t, tx)

	err := svc.UpdateBasics(context.Background(), userID, talentprofile.UpdateBasicsInput{})
	if !errors.Is(err, talentprofile.ErrNotTalent) {
		t.Fatalf("got %v, want ErrNotTalent", err)
	}
}

func TestSetRatesReplacesCurrentRatesAndClosesOldOnes(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)

	err := svc.SetRates(context.Background(), userID, []talentprofile.RateInput{
		{Amount: "500.00", CurrencyCode: "GHS", RateUnitCode: "per_event"},
	})
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SetRates(context.Background(), userID, []talentprofile.RateInput{
		{Amount: "700.00", CurrencyCode: "GHS", RateUnitCode: "per_event"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	tx.QueryRow(context.Background(), `
		SELECT count(*) FROM talent_rates r JOIN talent_profiles tp ON tp.id = r.talent_profile_id
		WHERE tp.user_id = $1 AND r.effective_to IS NULL`, userID).Scan(&count)
	if count != 1 {
		t.Fatalf("current rate count = %d, want 1", count)
	}
	var amount string
	tx.QueryRow(context.Background(), `
		SELECT r.amount::text FROM talent_rates r JOIN talent_profiles tp ON tp.id = r.talent_profile_id
		WHERE tp.user_id = $1 AND r.effective_to IS NULL`, userID).Scan(&amount)
	if amount != "700.00" {
		t.Fatalf("amount = %q, want 700.00", amount)
	}
}

func TestSetRatesRejectsUnknownRateUnitAndLeavesExistingRatesIntact(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)
	if err := svc.SetRates(context.Background(), userID, []talentprofile.RateInput{
		{Amount: "500.00", CurrencyCode: "GHS", RateUnitCode: "per_event"},
	}); err != nil {
		t.Fatal(err)
	}

	err := svc.SetRates(context.Background(), userID, []talentprofile.RateInput{
		{Amount: "1.00", CurrencyCode: "GHS", RateUnitCode: "not-a-real-unit"},
	})
	if !errors.Is(err, talentprofile.ErrUnknownCode) {
		t.Fatalf("got %v, want ErrUnknownCode", err)
	}

	var count int
	tx.QueryRow(context.Background(), `
		SELECT count(*) FROM talent_rates r JOIN talent_profiles tp ON tp.id = r.talent_profile_id
		WHERE tp.user_id = $1 AND r.effective_to IS NULL`, userID).Scan(&count)
	if count != 1 {
		t.Fatalf("current rate count = %d, want 1 (unchanged)", count)
	}
}

type recordingProvider struct {
	payments.FakeProvider
	gotCommission float64
	gotInput      payments.SubaccountInput
}

func (r *recordingProvider) CreateSubaccount(ctx context.Context, in payments.SubaccountInput, commission float64) (payments.Subaccount, error) {
	r.gotCommission = commission
	r.gotInput = in
	return r.FakeProvider.CreateSubaccount(ctx, in, commission)
}

func TestConnectPayoutCreatesSubaccountWithConfiguredCommissionAndStoresLast4(t *testing.T) {
	tx := newTx(t)
	provider := &recordingProvider{}
	svc := newService(tx, provider)
	userID := newTalentUser(t, tx)

	out, err := svc.ConnectPayout(context.Background(), userID, talentprofile.ConnectPayoutInput{
		BusinessName:   "Ama Serwaa",
		SettlementType: payments.SettlementBank,
		BankCode:       "058",
		AccountNumber:  "0123456047",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.AccountNumberLast4 != "6047" {
		t.Fatalf("AccountNumberLast4 = %q, want 6047", out.AccountNumberLast4)
	}
	if provider.gotCommission != 12.5 {
		t.Fatalf("commission sent to provider = %v, want 12.5", provider.gotCommission)
	}
	if provider.gotInput.AccountNumber != "0123456047" {
		t.Fatalf("provider got account number %q", provider.gotInput.AccountNumber)
	}

	var storedLast4, storedFullCheck string
	tx.QueryRow(context.Background(), `
		SELECT account_number_last4 FROM payout_accounts pa JOIN talent_profiles tp ON tp.id = pa.talent_profile_id
		WHERE tp.user_id = $1`, userID).Scan(&storedLast4)
	if storedLast4 != "6047" {
		t.Fatalf("stored last4 = %q", storedLast4)
	}
	// The full account number must never be persisted anywhere in payout_accounts.
	err = tx.QueryRow(context.Background(), `
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'payout_accounts' AND column_name ILIKE '%account_number%' AND column_name != 'account_number_last4'`).
		Scan(&storedFullCheck)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("payout_accounts has an unexpected account-number column: %q", storedFullCheck)
	}
}

func TestConnectPayoutIsIdempotentPerTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	userID := newTalentUser(t, tx)

	for i := 0; i < 2; i++ {
		_, err := svc.ConnectPayout(context.Background(), userID, talentprofile.ConnectPayoutInput{
			BusinessName: "Ama Serwaa", SettlementType: payments.SettlementBank, BankCode: "058", AccountNumber: "0123456047",
		})
		if err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	var count int
	tx.QueryRow(context.Background(), `
		SELECT count(*) FROM payout_accounts pa JOIN talent_profiles tp ON tp.id = pa.talent_profile_id
		WHERE tp.user_id = $1`, userID).Scan(&count)
	if count != 1 {
		t.Fatalf("payout_accounts rows = %d, want 1", count)
	}
}

func TestListSettlementBanksDelegatesToProvider(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx, payments.FakeProvider{})
	banks, err := svc.ListSettlementBanks(context.Background(), payments.SettlementMobileMoney)
	if err != nil {
		t.Fatal(err)
	}
	if len(banks) == 0 {
		t.Fatal("expected at least one mobile money provider from the fake")
	}
}
