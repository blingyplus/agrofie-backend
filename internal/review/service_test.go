package review_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/review"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like the other internal/* services: real transaction,
// always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed review tests")
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

// newCompletedBooking drives a real booking all the way to completed, so
// review tests exercise the review service against the real state the
// booking package produces, not a hand-inserted row.
func newCompletedBooking(t *testing.T, tx pgx.Tx) (organizerUserID, talentUserID, bookingID string) {
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
	past := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := avail.AddBlock(ctx, talentUserID, availability.AddBlockInput{StartsAt: past, EndsAt: past.AddDate(0, 0, 5), IsAvailable: true}); err != nil {
		t.Fatalf("add availability: %v", err)
	}

	bookings := booking.NewService(tx, avail)
	b, err := bookings.RequestBooking(ctx, organizerUserID, booking.RequestInput{
		TalentID: profileID, StartsAt: past.AddDate(0, 0, 1), EndsAt: past.AddDate(0, 0, 2), RateUnitCode: "per_event",
	})
	if err != nil {
		t.Fatalf("request booking: %v", err)
	}
	if _, err := bookings.AcceptBooking(ctx, talentUserID, b.ID); err != nil {
		t.Fatalf("accept booking: %v", err)
	}
	if _, err := bookings.MarkPaid(ctx, b.ID); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if _, err := bookings.CompleteBooking(ctx, organizerUserID, b.ID); err != nil {
		t.Fatalf("complete booking: %v", err)
	}
	return organizerUserID, talentUserID, b.ID
}

func TestSubmitRejectsInvalidRating(t *testing.T) {
	tx := newTx(t)
	svc := review.NewService(tx)
	organizer, _, bookingID := newCompletedBooking(t, tx)

	if _, err := svc.Submit(context.Background(), organizer, bookingID, 0, nil); !errors.Is(err, review.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
	if _, err := svc.Submit(context.Background(), organizer, bookingID, 6, nil); !errors.Is(err, review.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestSubmitRejectsUninvolvedCaller(t *testing.T) {
	tx := newTx(t)
	svc := review.NewService(tx)
	_, _, bookingID := newCompletedBooking(t, tx)
	stranger := newUser(t, tx, "Stranger")

	if _, err := svc.Submit(context.Background(), stranger, bookingID, 5, nil); !errors.Is(err, review.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestSubmitRejectsBeforeCompleted(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	avail := availability.NewService(tx)
	bookings := booking.NewService(tx, avail)
	reviews := review.NewService(tx)

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

	if _, err := reviews.Submit(ctx, organizer, b.ID, 5, nil); !errors.Is(err, review.ErrNotCompleted) {
		t.Fatalf("got %v, want ErrNotCompleted", err)
	}
}

func TestSubmitAllowsOneReviewPerSideAndRejectsDuplicate(t *testing.T) {
	tx := newTx(t)
	svc := review.NewService(tx)
	organizer, talentUser, bookingID := newCompletedBooking(t, tx)
	ctx := context.Background()

	body := "Great performance!"
	if _, err := svc.Submit(ctx, organizer, bookingID, 5, &body); err != nil {
		t.Fatalf("organizer Submit: %v", err)
	}
	if _, err := svc.Submit(ctx, talentUser, bookingID, 4, nil); err != nil {
		t.Fatalf("talent Submit: %v", err)
	}
	if _, err := svc.Submit(ctx, organizer, bookingID, 3, nil); !errors.Is(err, review.ErrAlreadyReviewed) {
		t.Fatalf("got %v, want ErrAlreadyReviewed", err)
	}

	rows, err := svc.ListForBooking(ctx, organizer, bookingID)
	if err != nil {
		t.Fatalf("ListForBooking: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d reviews, want 2", len(rows))
	}

	stranger := newUser(t, tx, "Stranger")
	if _, err := svc.ListForBooking(ctx, stranger, bookingID); !errors.Is(err, review.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestTalentSummaryOnlyCountsOrganizerReviews(t *testing.T) {
	tx := newTx(t)
	svc := review.NewService(tx)
	organizer, talentUser, bookingID := newCompletedBooking(t, tx)
	ctx := context.Background()

	var talentProfileID string
	if err := tx.QueryRow(ctx, `SELECT id::text FROM talent_profiles WHERE user_id = $1`, talentUser).Scan(&talentProfileID); err != nil {
		t.Fatalf("lookup talent profile: %v", err)
	}

	before, err := svc.TalentSummary(ctx, talentProfileID)
	if err != nil {
		t.Fatalf("TalentSummary (before): %v", err)
	}
	if before.ReviewCount != 0 {
		t.Fatalf("review count = %d, want 0", before.ReviewCount)
	}

	// Talent reviewing the organizer must NOT count toward the talent's own rating.
	if _, err := svc.Submit(ctx, talentUser, bookingID, 1, nil); err != nil {
		t.Fatalf("talent Submit: %v", err)
	}
	afterTalentReview, err := svc.TalentSummary(ctx, talentProfileID)
	if err != nil {
		t.Fatalf("TalentSummary: %v", err)
	}
	if afterTalentReview.ReviewCount != 0 {
		t.Fatalf("review count = %d, want 0 (talent's own review of the organizer shouldn't count)", afterTalentReview.ReviewCount)
	}

	// The organizer's review of the talent should count.
	if _, err := svc.Submit(ctx, organizer, bookingID, 5, nil); err != nil {
		t.Fatalf("organizer Submit: %v", err)
	}
	after, err := svc.TalentSummary(ctx, talentProfileID)
	if err != nil {
		t.Fatalf("TalentSummary (after): %v", err)
	}
	if after.ReviewCount != 1 || after.AverageRating != 5 {
		t.Fatalf("got %+v, want {1 5}", after)
	}
}
