package dispute_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/dispute"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like the other internal/* services: real transaction,
// always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed dispute tests")
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

// newAgreedBooking drives a real booking to agreed, so dispute tests
// exercise the dispute service against the real state the booking package
// produces, not a hand-inserted row.
func newAgreedBooking(t *testing.T, tx pgx.Tx) (organizerUserID, talentUserID, bookingID string) {
	t.Helper()
	ctx := context.Background()
	organizerUserID = newUser(t, tx, "Organizer")
	talentUserID = newUser(t, tx, "Talent")

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
	from := time.Date(2026, time.December, 1, 0, 0, 0, 0, time.UTC)
	if _, err := avail.AddBlock(ctx, talentUserID, availability.AddBlockInput{StartsAt: from, EndsAt: from.AddDate(0, 0, 30), IsAvailable: true}); err != nil {
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
	return organizerUserID, talentUserID, b.ID
}

func TestOpenRejectsBeforeAgreed(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	avail := availability.NewService(tx)
	bookings := booking.NewService(tx, avail)
	svc := dispute.NewService(tx)

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

	if _, err := svc.Open(ctx, organizer, b.ID, "no_show", nil); !errors.Is(err, dispute.ErrNotDisputable) {
		t.Fatalf("got %v, want ErrNotDisputable", err)
	}
}

func TestOpenRejectsUninvolvedCaller(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	_, _, bookingID := newAgreedBooking(t, tx)
	stranger := newUser(t, tx, "Stranger")

	if _, err := svc.Open(ctx, stranger, bookingID, "no_show", nil); !errors.Is(err, dispute.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestOpenRejectsASecondOpenDispute(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	organizer, talentUser, bookingID := newAgreedBooking(t, tx)

	if _, err := svc.Open(ctx, organizer, bookingID, "no_show", nil); err != nil {
		t.Fatalf("first Open: %v", err)
	}
	if _, err := svc.Open(ctx, talentUser, bookingID, "payment", nil); !errors.Is(err, dispute.ErrAlreadyOpen) {
		t.Fatalf("got %v, want ErrAlreadyOpen", err)
	}
}

func TestListForBookingVisibleOnlyToParties(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	organizer, talentUser, bookingID := newAgreedBooking(t, tx)
	details := "Didn't show up."

	if _, err := svc.Open(ctx, organizer, bookingID, "no_show", &details); err != nil {
		t.Fatalf("Open: %v", err)
	}

	rows, err := svc.ListForBooking(ctx, talentUser, bookingID)
	if err != nil {
		t.Fatalf("ListForBooking (talent): %v", err)
	}
	if len(rows) != 1 || rows[0].StatusCode != "open" || rows[0].ReasonCode != "no_show" {
		t.Fatalf("got %+v, want one open no_show dispute", rows)
	}

	stranger := newUser(t, tx, "Stranger")
	if _, err := svc.ListForBooking(ctx, stranger, bookingID); !errors.Is(err, dispute.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestResolveRejectsUnknownStatus(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	organizer, _, bookingID := newAgreedBooking(t, tx)
	admin := newUser(t, tx, "Admin")

	d, err := svc.Open(ctx, organizer, bookingID, "no_show", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := svc.Resolve(ctx, admin, d.ID, "open", nil); !errors.Is(err, dispute.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput (open is not a resolve target)", err)
	}
	if _, err := svc.Resolve(ctx, admin, d.ID, "bogus", nil); !errors.Is(err, dispute.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestResolveUpdatesStatusAndNoteAndAppearsForAdmin(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	organizer, _, bookingID := newAgreedBooking(t, tx)
	admin := newUser(t, tx, "Admin")

	d, err := svc.Open(ctx, organizer, bookingID, "quality", nil)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	note := "Talent gave a partial refund off-platform; considered settled."
	resolved, err := svc.Resolve(ctx, admin, d.ID, "resolved", &note)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.StatusCode != "resolved" || resolved.ResolutionNote == nil || *resolved.ResolutionNote != note {
		t.Fatalf("got %+v, want status resolved with the note", resolved)
	}

	adminRows, err := svc.ListForAdmin(ctx, nil)
	if err != nil {
		t.Fatalf("ListForAdmin: %v", err)
	}
	found := false
	for _, item := range adminRows {
		if item.ID == d.ID {
			found = true
			if item.OrganizerDisplayName != "Organizer" || item.BookingStatusCode != "agreed" {
				t.Fatalf("got %+v, want organizer context attached", item)
			}
		}
	}
	if !found {
		t.Fatalf("resolved dispute %s not found in admin list", d.ID)
	}

	statusFilter := "resolved"
	filtered, err := svc.ListForAdmin(ctx, &statusFilter)
	if err != nil {
		t.Fatalf("ListForAdmin (filtered): %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != d.ID {
		t.Fatalf("got %+v, want only the resolved dispute", filtered)
	}
}

func TestResolveRejectsUnknownDispute(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := dispute.NewService(tx)
	admin := newUser(t, tx, "Admin")

	if _, err := svc.Resolve(ctx, admin, "00000000-0000-0000-0000-000000000000", "resolved", nil); !errors.Is(err, dispute.ErrDisputeNotFound) {
		t.Fatalf("got %v, want ErrDisputeNotFound", err)
	}
}
