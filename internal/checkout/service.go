// Package checkout orchestrates Paystack split checkout for an agreed
// booking: it never moves money itself, only starts a Checkout session and
// reacts to Paystack's own verified confirmation (a signed webhook, or the
// VerifyTransaction fallback) by moving the booking to paid via
// booking.Service.MarkPaid. See docs/PRODUCT.md (Money model) and
// docs/ARCHITECTURE.md.
package checkout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var (
	ErrInvalidInput   = errors.New("invalid input")
	ErrForbidden      = errors.New("not allowed")
	ErrNotFound       = errors.New("booking not found")
	ErrNotAgreed      = errors.New("booking is not agreed")
	ErrPayoutNotReady = errors.New("talent has no active payout account")
	ErrNoPayment      = errors.New("no payment has been started for this booking")
)

type beginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db                beginner
	provider          payments.Provider
	bookings          *booking.Service
	commissionPercent float64
}

func NewService(conn beginner, provider payments.Provider, bookings *booking.Service, commissionPercent float64) *Service {
	return &Service{db: conn, provider: provider, bookings: bookings, commissionPercent: commissionPercent}
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

// Checkout is what InitiateCheckout hands back to start payment.
type Checkout struct {
	CheckoutURL       string
	Reference         string
	AmountPesewas     int64
	CommissionPesewas int64
}

// InitiateCheckout starts a new Paystack split transaction for an agreed
// booking. Only the organizer who made the booking can pay for it. The
// amount is never taken from the client: it is the booking's own
// server-snapshotted quoted_amount, converted to pesewas.
func (s *Service) InitiateCheckout(ctx context.Context, organizerUserID, bookingID string) (*Checkout, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}

	var out *Checkout
	err = s.withTx(ctx, func(q *db.Queries) error {
		row, err := q.GetBookingForCheckout(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(row.OrganizerUserID) != organizerUserID {
			return ErrForbidden
		}
		if row.StatusCode != "agreed" {
			return ErrNotAgreed
		}
		if row.ProviderSubaccountCode == nil || row.PayoutActive == nil || !*row.PayoutActive {
			return ErrPayoutNotReady
		}
		if row.OrganizerEmail == nil || *row.OrganizerEmail == "" {
			return fmt.Errorf("%w: organizer has no email on file", ErrInvalidInput)
		}

		amountPesewas, err := decimalToPesewas(row.QuotedAmount)
		if err != nil {
			return fmt.Errorf("parse quoted amount: %w", err)
		}
		commissionPesewas := int64(math.Round(float64(amountPesewas) * s.commissionPercent / 100))
		currency := strOr(row.CurrencyCode)
		reference := "agrofie_" + uuid.NewString()

		checkout, err := s.provider.InitializeCheckout(ctx, payments.CheckoutInput{
			Reference:         reference,
			PayerEmail:        *row.OrganizerEmail,
			AmountPesewas:     amountPesewas,
			CommissionPesewas: commissionPesewas,
			SubaccountCode:    *row.ProviderSubaccountCode,
			CurrencyCode:      currency,
		})
		if err != nil {
			return fmt.Errorf("initialize checkout: %w", err)
		}
		// The reference the provider actually settled on is authoritative;
		// FakeProvider and Paystack both echo back what was sent, but don't
		// assume that.
		ref := checkout.Reference
		if ref == "" {
			ref = reference
		}
		url := checkout.CheckoutURL

		if _, err := q.InsertPayment(ctx, db.InsertPaymentParams{
			BookingID: bID, ProviderReference: ref,
			AmountPesewas: amountPesewas, CommissionPesewas: commissionPesewas,
			CurrencyCode: currency, CheckoutUrl: &url,
		}); err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}

		out = &Checkout{CheckoutURL: url, Reference: ref, AmountPesewas: amountPesewas, CommissionPesewas: commissionPesewas}
		return nil
	})
	return out, err
}

// PaymentStatus is the current state of the latest payment attempt for a
// booking.
type PaymentStatus struct {
	Reference         string
	StatusCode        string
	StatusName        string
	AmountPesewas     int64
	CommissionPesewas int64
	CurrencyCode      string
	CheckoutURL       *string
}

// LatestPayment reads the latest payment attempt for a booking without
// contacting Paystack (unlike VerifyPayment). Either party to the booking
// may read it. Returns ErrNoPayment if checkout was never started.
func (s *Service) LatestPayment(ctx context.Context, callerUserID, bookingID string) (*PaymentStatus, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}
	var out *PaymentStatus
	err = s.withTx(ctx, func(q *db.Queries) error {
		detail, err := q.GetBookingDetail(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(detail.OrganizerUserID) != callerUserID && uuidString(detail.TalentUserID) != callerUserID {
			return ErrForbidden
		}
		payment, err := q.GetLatestPaymentForBooking(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPayment
		}
		if err != nil {
			return fmt.Errorf("get payment: %w", err)
		}
		out = &PaymentStatus{
			Reference: payment.ProviderReference, StatusCode: payment.StatusCode, StatusName: payment.StatusName,
			AmountPesewas: payment.AmountPesewas, CommissionPesewas: payment.CommissionPesewas,
			CurrencyCode: payment.CurrencyCode, CheckoutURL: payment.CheckoutUrl,
		}
		return nil
	})
	return out, err
}

// VerifyPayment is the reconciliation fallback for when a webhook hasn't
// arrived: it asks Paystack directly for the transaction status and, if
// Paystack says it succeeded, applies the same effects a webhook would.
func (s *Service) VerifyPayment(ctx context.Context, callerUserID, bookingID string) (*PaymentStatus, error) {
	bID, err := parseUUID(bookingID)
	if err != nil {
		return nil, ErrNotFound
	}

	var reference string
	var out *PaymentStatus
	err = s.withTx(ctx, func(q *db.Queries) error {
		b, err := q.GetBookingForCheckout(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get booking: %w", err)
		}
		if uuidString(b.OrganizerUserID) != callerUserID {
			return ErrForbidden
		}
		payment, err := q.GetLatestPaymentForBooking(ctx, bID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPayment
		}
		if err != nil {
			return fmt.Errorf("get payment: %w", err)
		}
		reference = payment.ProviderReference
		out = &PaymentStatus{
			Reference: payment.ProviderReference, StatusCode: payment.StatusCode, StatusName: payment.StatusName,
			AmountPesewas: payment.AmountPesewas, CommissionPesewas: payment.CommissionPesewas,
			CurrencyCode: payment.CurrencyCode, CheckoutURL: payment.CheckoutUrl,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	status, err := s.provider.VerifyTransaction(ctx, reference)
	if err != nil {
		return out, fmt.Errorf("verify transaction: %w", err)
	}
	if status == "success" {
		if err := s.applySuccess(ctx, reference, []byte(`{"source":"verify"}`)); err != nil {
			return out, err
		}
		out.StatusCode, out.StatusName = "succeeded", "Succeeded"
	} else if status == "failed" || status == "abandoned" {
		if err := s.applyFailure(ctx, reference, []byte(`{"source":"verify"}`)); err != nil {
			return out, err
		}
		out.StatusCode, out.StatusName = "failed", "Failed"
	}
	return out, nil
}

// webhookPayload is the slice of Paystack's webhook body this service
// cares about. Anything else in the body is stored as-is in payment_events
// but not parsed.
type webhookPayload struct {
	Event string `json:"event"`
	Data  struct {
		Reference string `json:"reference"`
		Status    string `json:"status"`
	} `json:"data"`
}

// HandleWebhookEvent verifies the Paystack signature, records the event
// (idempotently — a retried delivery is a no-op), and on a successful
// charge moves the booking to paid. Never trust a webhook body before the
// signature check passes.
func (s *Service) HandleWebhookEvent(ctx context.Context, rawBody []byte, signatureHeader string) error {
	if !s.provider.VerifyWebhookSignature(rawBody, signatureHeader) {
		return fmt.Errorf("%w: invalid webhook signature", ErrInvalidInput)
	}
	var payload webhookPayload
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return fmt.Errorf("%w: malformed webhook body", ErrInvalidInput)
	}
	if payload.Data.Reference == "" || payload.Event == "" {
		return fmt.Errorf("%w: webhook missing event/reference", ErrInvalidInput)
	}

	handled := false
	err := s.withTx(ctx, func(q *db.Queries) error {
		rows, err := q.InsertPaymentEvent(ctx, db.InsertPaymentEventParams{
			ProviderReference: payload.Data.Reference, EventType: payload.Event, Payload: rawBody,
		})
		if err != nil {
			return fmt.Errorf("insert payment event: %w", err)
		}
		handled = rows > 0
		return nil
	})
	if err != nil {
		return err
	}
	if !handled {
		return nil // already processed this exact event; no-op
	}

	switch payload.Event {
	case "charge.success":
		return s.applySuccess(ctx, payload.Data.Reference, rawBody)
	case "charge.failed":
		return s.applyFailure(ctx, payload.Data.Reference, rawBody)
	default:
		return nil // recorded, nothing else to do for this event type
	}
}

func (s *Service) applySuccess(ctx context.Context, reference string, _ []byte) error {
	var bookingID string
	err := s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.UpdatePaymentStatusByReference(ctx, db.UpdatePaymentStatusByReferenceParams{
			StatusCode: "succeeded", ProviderReference: reference,
		}); err != nil {
			return fmt.Errorf("update payment status: %w", err)
		}
		p, err := q.GetPaymentByReference(ctx, reference)
		if err != nil {
			return fmt.Errorf("get payment: %w", err)
		}
		bookingID = uuidString(p.BookingID)
		return nil
	})
	if err != nil {
		return err
	}
	// MarkPaid guards on status == agreed; a booking already paid (a
	// duplicate success event, or verify racing the webhook) is a no-op,
	// not an error.
	if _, err := s.bookings.MarkPaid(ctx, bookingID); err != nil && !errors.Is(err, booking.ErrNotPending) {
		return fmt.Errorf("mark paid: %w", err)
	}
	return nil
}

func (s *Service) applyFailure(ctx context.Context, reference string, _ []byte) error {
	return s.withTx(ctx, func(q *db.Queries) error {
		if _, err := q.UpdatePaymentStatusByReference(ctx, db.UpdatePaymentStatusByReferenceParams{
			StatusCode: "failed", ProviderReference: reference,
		}); err != nil {
			return fmt.Errorf("update payment status: %w", err)
		}
		return nil
	})
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

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// decimalToPesewas converts a decimal string amount (e.g. "4500", "4500.5",
// "4500.50") into integer pesewas, without going through float64 (money is
// never rounded through binary floating point). Amounts beyond 2 decimal
// places are truncated, matching the NUMERIC(12,2) column this reads from.
func decimalToPesewas(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty amount")
	}
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	for len(frac) < 2 {
		frac += "0"
	}
	frac = frac[:2]

	wholeN, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", s, err)
	}
	fracN, err := strconv.ParseInt(frac, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", s, err)
	}
	total := wholeN*100 + fracN
	if neg {
		total = -total
	}
	return total, nil
}
