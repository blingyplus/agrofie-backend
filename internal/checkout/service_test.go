package checkout_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/checkout"
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like the other internal/* services: real transaction,
// always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed checkout tests")
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

func newUser(t *testing.T, tx pgx.Tx, name string) string {
	t.Helper()
	email := uniq("u") + "@example.test"
	var userID string
	err := tx.QueryRow(context.Background(), `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT $1, id, gen_random_uuid() FROM user_statuses WHERE code = 'active'
		RETURNING id::text`, email).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2)`, userID, name); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return userID
}

// stubProvider is a payments.Provider test double that records what it was
// called with and lets each test control what VerifyTransaction reports.
type stubProvider struct {
	checkoutURL    string
	verifyStatus   payments.TransactionStatus
	lastCheckoutIn payments.CheckoutInput
	secretKey      string
}

func (s *stubProvider) ListSettlementBanks(context.Context, string, payments.SettlementType) ([]payments.Bank, error) {
	return nil, nil
}
func (s *stubProvider) CreateSubaccount(context.Context, payments.SubaccountInput, float64) (payments.Subaccount, error) {
	return payments.Subaccount{}, nil
}
func (s *stubProvider) InitializeCheckout(_ context.Context, in payments.CheckoutInput) (payments.Checkout, error) {
	s.lastCheckoutIn = in
	url := s.checkoutURL
	if url == "" {
		url = "https://checkout.test/" + in.Reference
	}
	return payments.Checkout{CheckoutURL: url, Reference: in.Reference}, nil
}
func (s *stubProvider) VerifyTransaction(context.Context, string) (payments.TransactionStatus, error) {
	return s.verifyStatus, nil
}
func (s *stubProvider) VerifyWebhookSignature(rawBody []byte, signatureHeader string) bool {
	mac := hmac.New(sha512.New, []byte(s.secretKey))
	mac.Write(rawBody)
	return hex.EncodeToString(mac.Sum(nil)) == signatureHeader
}

var _ payments.Provider = (*stubProvider)(nil)

func sign(secret string, body []byte) string {
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// newAgreedBooking builds an organizer + bookable, payout-ready talent, then
// drives a real booking through inquiry -> agreed so it's ready to pay.
func newAgreedBooking(t *testing.T, tx pgx.Tx) (organizerUserID, bookingID string) {
	t.Helper()
	ctx := context.Background()
	organizerUserID = newUser(t, tx, "Organizer")
	talentUserID := newUser(t, tx, "Talent")

	var profileID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO talent_profiles (user_id, is_searchable) VALUES ($1, true) RETURNING id::text`, talentUserID).Scan(&profileID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payout_accounts (talent_profile_id, settlement_type, bank_code, bank_name, account_number_last4, account_name, provider_subaccount_code)
		VALUES ($1, 'bank', '058', 'Test Bank', '1234', 'Talent', 'ACCT_test')`, profileID); err != nil {
		t.Fatalf("insert payout account: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id)
		SELECT $1, 500.00, cur.id, ru.id FROM currencies cur, rate_units ru WHERE cur.code = 'GHS' AND ru.code = 'per_event'`, profileID); err != nil {
		t.Fatalf("insert rate: %v", err)
	}

	avail := availability.NewService(tx)
	from, to := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.December, 30, 0, 0, 0, 0, time.UTC)
	if _, err := avail.AddBlock(ctx, talentUserID, availability.AddBlockInput{StartsAt: from, EndsAt: to, IsAvailable: true}); err != nil {
		t.Fatalf("add availability: %v", err)
	}

	bookings := booking.NewService(tx, avail)
	b, err := bookings.RequestBooking(ctx, organizerUserID, booking.RequestInput{
		TalentID: profileID, StartsAt: from.AddDate(0, 0, 5), EndsAt: from.AddDate(0, 0, 6), RateUnitCode: "per_event",
	})
	if err != nil {
		t.Fatalf("request booking: %v", err)
	}
	if _, err := bookings.AcceptBooking(ctx, talentUserID, b.ID); err != nil {
		t.Fatalf("accept booking: %v", err)
	}
	return organizerUserID, b.ID
}

func newServices(tx pgx.Tx, provider payments.Provider) (*checkout.Service, *booking.Service) {
	avail := availability.NewService(tx)
	bookings := booking.NewService(tx, avail)
	return checkout.NewService(tx, provider, bookings, 12.5), bookings
}

func TestInitiateCheckoutComputesAmountAndCommissionInPesewas(t *testing.T) {
	tx := newTx(t)
	provider := &stubProvider{}
	svc, _ := newServices(tx, provider)
	organizer, bookingID := newAgreedBooking(t, tx)

	out, err := svc.InitiateCheckout(context.Background(), organizer, bookingID)
	if err != nil {
		t.Fatalf("InitiateCheckout: %v", err)
	}
	// Rate was 500.00 GHS -> 50000 pesewas; 12.5% commission -> 6250 pesewas.
	if out.AmountPesewas != 50000 {
		t.Fatalf("amount = %d, want 50000", out.AmountPesewas)
	}
	if out.CommissionPesewas != 6250 {
		t.Fatalf("commission = %d, want 6250", out.CommissionPesewas)
	}
	if out.CheckoutURL == "" || out.Reference == "" {
		t.Fatalf("expected checkout url/reference, got %+v", out)
	}
	if provider.lastCheckoutIn.SubaccountCode != "ACCT_test" {
		t.Fatalf("subaccount = %q, want ACCT_test", provider.lastCheckoutIn.SubaccountCode)
	}
	if provider.lastCheckoutIn.PayerEmail == "" {
		t.Fatalf("expected payer email to be set")
	}
}

func TestInitiateCheckoutRejectsNonOrganizer(t *testing.T) {
	tx := newTx(t)
	svc, _ := newServices(tx, &stubProvider{})
	_, bookingID := newAgreedBooking(t, tx)
	someoneElse := newUser(t, tx, "Stranger")

	_, err := svc.InitiateCheckout(context.Background(), someoneElse, bookingID)
	if !errors.Is(err, checkout.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestInitiateCheckoutRejectsUnagreedBooking(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	avail := availability.NewService(tx)
	bookings := booking.NewService(tx, avail)
	svc := checkout.NewService(tx, &stubProvider{}, bookings, 12.5)

	organizer := newUser(t, tx, "Organizer")
	talentUser := newUser(t, tx, "Talent")
	var profileID string
	if err := tx.QueryRow(ctx, `INSERT INTO talent_profiles (user_id, is_searchable) VALUES ($1, true) RETURNING id::text`, talentUser).Scan(&profileID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO payout_accounts (talent_profile_id, settlement_type, bank_code, bank_name, account_number_last4, account_name, provider_subaccount_code)
		VALUES ($1, 'bank', '058', 'Test Bank', '1234', 'Talent', 'ACCT_test')`, profileID); err != nil {
		t.Fatalf("insert payout account: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id)
		SELECT $1, 500.00, cur.id, ru.id FROM currencies cur, rate_units ru WHERE cur.code = 'GHS' AND ru.code = 'per_event'`, profileID); err != nil {
		t.Fatalf("insert rate: %v", err)
	}
	from := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	if _, err := avail.AddBlock(ctx, talentUser, availability.AddBlockInput{StartsAt: from, EndsAt: from.AddDate(0, 0, 30), IsAvailable: true}); err != nil {
		t.Fatalf("add availability: %v", err)
	}
	b, err := bookings.RequestBooking(ctx, organizer, booking.RequestInput{
		TalentID: profileID, StartsAt: from.AddDate(0, 0, 5), EndsAt: from.AddDate(0, 0, 6), RateUnitCode: "per_event",
	})
	if err != nil {
		t.Fatalf("request booking: %v", err)
	}

	_, err = svc.InitiateCheckout(ctx, organizer, b.ID)
	if !errors.Is(err, checkout.ErrNotAgreed) {
		t.Fatalf("got %v, want ErrNotAgreed", err)
	}
}

func TestHandleWebhookEventRejectsBadSignature(t *testing.T) {
	tx := newTx(t)
	provider := &stubProvider{secretKey: "sk_test_x"}
	svc, _ := newServices(tx, provider)

	body := []byte(`{"event":"charge.success","data":{"reference":"whatever","status":"success"}}`)
	err := svc.HandleWebhookEvent(context.Background(), body, "not-the-right-signature")
	if !errors.Is(err, checkout.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestHandleWebhookEventMarksBookingPaidOnChargeSuccess(t *testing.T) {
	tx := newTx(t)
	provider := &stubProvider{secretKey: "sk_test_x"}
	svc, bookings := newServices(tx, provider)
	organizer, bookingID := newAgreedBooking(t, tx)

	out, err := svc.InitiateCheckout(context.Background(), organizer, bookingID)
	if err != nil {
		t.Fatalf("InitiateCheckout: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"event": "charge.success",
		"data":  map[string]any{"reference": out.Reference, "status": "success"},
	})
	sig := sign(provider.secretKey, body)
	if err := svc.HandleWebhookEvent(context.Background(), body, sig); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}

	detail, err := bookings.GetDetail(context.Background(), organizer, bookingID)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.StatusCode != "paid" {
		t.Fatalf("booking status = %q, want paid", detail.StatusCode)
	}

	// A retried delivery of the exact same event is a no-op, not an error,
	// and doesn't fail trying to re-transition an already-paid booking.
	if err := svc.HandleWebhookEvent(context.Background(), body, sig); err != nil {
		t.Fatalf("HandleWebhookEvent (retry): %v", err)
	}
}

func TestVerifyPaymentAppliesPaystackFallbackStatus(t *testing.T) {
	tx := newTx(t)
	provider := &stubProvider{verifyStatus: "success"}
	svc, bookings := newServices(tx, provider)
	organizer, bookingID := newAgreedBooking(t, tx)

	if _, err := svc.InitiateCheckout(context.Background(), organizer, bookingID); err != nil {
		t.Fatalf("InitiateCheckout: %v", err)
	}

	status, err := svc.VerifyPayment(context.Background(), organizer, bookingID)
	if err != nil {
		t.Fatalf("VerifyPayment: %v", err)
	}
	if status.StatusCode != "succeeded" {
		t.Fatalf("status = %q, want succeeded", status.StatusCode)
	}

	detail, err := bookings.GetDetail(context.Background(), organizer, bookingID)
	if err != nil {
		t.Fatalf("GetDetail: %v", err)
	}
	if detail.StatusCode != "paid" {
		t.Fatalf("booking status = %q, want paid", detail.StatusCode)
	}
}
