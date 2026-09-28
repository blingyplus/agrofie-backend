package booking_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like the other internal/* services: real transaction,
// always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed booking tests")
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

// newBookableTalent creates a searchable talent with a connected payout
// account and one published rate, ready to be booked.
func newBookableTalent(t *testing.T, tx pgx.Tx, name string) (userID, profileID string) {
	t.Helper()
	userID = newUser(t, tx, name)
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO talent_profiles (user_id, is_searchable) VALUES ($1, true) RETURNING id::text`, userID).Scan(&profileID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO payout_accounts (talent_profile_id, settlement_type, bank_code, bank_name, account_number_last4, account_name, provider_subaccount_code)
		VALUES ($1, 'bank', '058', 'Test Bank', '1234', $2, 'ACCT_test')`, profileID, name); err != nil {
		t.Fatalf("insert payout account: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id)
		SELECT $1, 500.00, cur.id, ru.id FROM currencies cur, rate_units ru WHERE cur.code = 'GHS' AND ru.code = 'per_event'`, profileID); err != nil {
		t.Fatalf("insert rate: %v", err)
	}
	return userID, profileID
}

func makeAvailable(t *testing.T, tx pgx.Tx, talentUserID string, from, to time.Time) {
	t.Helper()
	avail := availability.NewService(tx)
	if _, err := avail.AddBlock(context.Background(), talentUserID, availability.AddBlockInput{
		StartsAt: from, EndsAt: to, IsAvailable: true,
	}); err != nil {
		t.Fatalf("add availability: %v", err)
	}
}

func newService(tx pgx.Tx) *booking.Service {
	return booking.NewService(tx, availability.NewService(tx))
}

func day(offset int) time.Time {
	return time.Date(2026, time.December, 1+offset, 0, 0, 0, 0, time.UTC)
}

func validInput(talentID string) booking.RequestInput {
	return booking.RequestInput{
		TalentID:     talentID,
		StartsAt:     day(10),
		EndsAt:       day(11),
		RateUnitCode: "per_event",
	}
}

func TestRequestBookingRejectsEndBeforeStart(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	_, profileID := newBookableTalent(t, tx, "Talent")

	in := validInput(profileID)
	in.StartsAt, in.EndsAt = day(11), day(10)
	_, err := svc.RequestBooking(context.Background(), organizer, in)
	if !errors.Is(err, booking.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestRequestBookingRejectsUnknownRateUnit(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))

	in := validInput(profileID)
	in.RateUnitCode = "not-a-real-unit"
	_, err := svc.RequestBooking(context.Background(), organizer, in)
	if !errors.Is(err, booking.ErrRateNotFound) {
		t.Fatalf("got %v, want ErrRateNotFound", err)
	}
}

func TestRequestBookingRejectsNonSearchableTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser := newUser(t, tx, "Hidden Talent")
	var profileID string
	if err := tx.QueryRow(context.Background(), `INSERT INTO talent_profiles (user_id, is_searchable) VALUES ($1, false) RETURNING id::text`, talentUser).Scan(&profileID); err != nil {
		t.Fatal(err)
	}

	_, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if !errors.Is(err, booking.ErrTalentNotBookable) {
		t.Fatalf("got %v, want ErrTalentNotBookable", err)
	}
}

func TestRequestBookingRejectsTalentWithoutPayoutAccount(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser := newUser(t, tx, "No Payout Talent")
	var profileID string
	if err := tx.QueryRow(context.Background(), `INSERT INTO talent_profiles (user_id, is_searchable) VALUES ($1, true) RETURNING id::text`, talentUser).Scan(&profileID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id)
		SELECT $1, 500.00, cur.id, ru.id FROM currencies cur, rate_units ru WHERE cur.code = 'GHS' AND ru.code = 'per_event'`, profileID); err != nil {
		t.Fatal(err)
	}

	_, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if !errors.Is(err, booking.ErrTalentNotBookable) {
		t.Fatalf("got %v, want ErrTalentNotBookable", err)
	}
}

func TestRequestBookingRejectsWhenTalentHasNotPublishedAvailability(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	_, profileID := newBookableTalent(t, tx, "Talent")
	// No availability block added.

	_, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if !errors.Is(err, booking.ErrNotAvailable) {
		t.Fatalf("got %v, want ErrNotAvailable", err)
	}
}

func TestRequestBookingSnapshotsQuoteAndCreatesInquiry(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))

	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	if b.StatusCode != "inquiry" || b.QuotedAmount != "500.00" || b.CurrencyCode != "GHS" {
		t.Fatalf("got %+v", b)
	}
}

func TestRequestBookingRejectsOverlapWithAnExistingAgreedBooking(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer1 := newUser(t, tx, "Organizer1")
	organizer2 := newUser(t, tx, "Organizer2")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))

	first, err := svc.RequestBooking(context.Background(), organizer1, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, first.ID); err != nil {
		t.Fatal(err)
	}

	_, err = svc.RequestBooking(context.Background(), organizer2, validInput(profileID))
	if !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("got %v, want ErrConflict", err)
	}
}

func TestTwoPendingInquiriesForTheSameSlotAreBothAllowed(t *testing.T) {
	// Two organizers can both inquire about the same slot; only one can be
	// accepted (the second accept then fails as a conflict).
	tx := newTx(t)
	svc := newService(tx)
	organizer1 := newUser(t, tx, "Organizer1")
	organizer2 := newUser(t, tx, "Organizer2")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))

	a, err := svc.RequestBooking(context.Background(), organizer1, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.RequestBooking(context.Background(), organizer2, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AcceptBooking(context.Background(), talentUser, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err = svc.AcceptBooking(context.Background(), talentUser, b.ID)
	if !errors.Is(err, booking.ErrConflict) {
		t.Fatalf("accepting the second overlapping inquiry: got %v, want ErrConflict", err)
	}
}

func TestAcceptBookingCreatesContractAndRejectsNonOwner(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	otherTalentUser, _ := newBookableTalent(t, tx, "Other Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))

	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.AcceptBooking(context.Background(), otherTalentUser, b.ID); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("wrong talent accepting: got %v, want ErrForbidden", err)
	}

	out, err := svc.AcceptBooking(context.Background(), talentUser, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != "agreed" {
		t.Fatalf("status = %q, want agreed", out.StatusCode)
	}

	detail, err := svc.GetDetail(context.Background(), talentUser, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.TermsText == nil || *detail.TermsText == "" {
		t.Fatalf("expected a contract terms snapshot, got %+v", detail)
	}
}

func TestAcceptingAlreadyDecidedBookingFails(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, b.ID); !errors.Is(err, booking.ErrNotPending) {
		t.Fatalf("got %v, want ErrNotPending", err)
	}
}

func TestDeclineBookingSetsCancelledWithTalentReason(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.DeclineBooking(context.Background(), talentUser, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != "cancelled" {
		t.Fatalf("status = %q, want cancelled", out.StatusCode)
	}
}

func TestCancelBookingByOrganizerOnlyWorksWhilePending(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, b.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelBooking(context.Background(), organizer, b.ID); !errors.Is(err, booking.ErrNotPending) {
		t.Fatalf("cancel after agreed: got %v, want ErrNotPending", err)
	}
}

func TestCancelBookingRejectsNonOwnerOrganizer(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	otherOrganizer := newUser(t, tx, "Other Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.CancelBooking(context.Background(), otherOrganizer, b.ID); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
}

func TestListMyBookingsAsOrganizerAndAsTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	asOrganizer, err := svc.ListMyBookingsAsOrganizer(context.Background(), organizer)
	if err != nil {
		t.Fatal(err)
	}
	if len(asOrganizer) != 1 || asOrganizer[0].ID != b.ID {
		t.Fatalf("got %+v", asOrganizer)
	}

	asTalent, err := svc.ListMyBookingsAsTalent(context.Background(), talentUser)
	if err != nil {
		t.Fatal(err)
	}
	if len(asTalent) != 1 || asTalent[0].ID != b.ID {
		t.Fatalf("got %+v", asTalent)
	}
}

func TestGetDetailRejectsUninvolvedCaller(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	stranger := newUser(t, tx, "Stranger")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := svc.GetDetail(context.Background(), stranger, b.ID); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}
	if _, err := svc.GetDetail(context.Background(), organizer, b.ID); err != nil {
		t.Fatalf("organizer should see own booking: %v", err)
	}
	if _, err := svc.GetDetail(context.Background(), talentUser, b.ID); err != nil {
		t.Fatalf("talent should see own booking: %v", err)
	}
}

func TestCompleteBookingRequiresPaidAndEventOver(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	makeAvailable(t, tx, talentUser, day(0), day(30))
	b, err := svc.RequestBooking(context.Background(), organizer, validInput(profileID))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, b.ID); err != nil {
		t.Fatal(err)
	}

	// Not paid yet: rejected.
	if _, err := svc.CompleteBooking(context.Background(), organizer, b.ID); !errors.Is(err, booking.ErrNotPending) {
		t.Fatalf("got %v, want ErrNotPending", err)
	}

	if _, err := svc.MarkPaid(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}

	// Paid, but the event (day(10)-day(11), in December 2026) hasn't
	// happened yet: rejected.
	if _, err := svc.CompleteBooking(context.Background(), organizer, b.ID); !errors.Is(err, booking.ErrEventNotYetOver) {
		t.Fatalf("got %v, want ErrEventNotYetOver", err)
	}
}

func TestCompleteBookingAllowsEitherPartyOnceEventIsOver(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newUser(t, tx, "Organizer")
	talentUser, profileID := newBookableTalent(t, tx, "Talent")
	// A window safely in the past relative to "now", so CompleteBooking's
	// event-over guard passes.
	past := time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC)
	if _, err := availability.NewService(tx).AddBlock(context.Background(), talentUser, availability.AddBlockInput{
		StartsAt: past, EndsAt: past.AddDate(0, 0, 5), IsAvailable: true,
	}); err != nil {
		t.Fatalf("add availability: %v", err)
	}
	b, err := svc.RequestBooking(context.Background(), organizer, booking.RequestInput{
		TalentID: profileID, StartsAt: past.AddDate(0, 0, 1), EndsAt: past.AddDate(0, 0, 2), RateUnitCode: "per_event",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AcceptBooking(context.Background(), talentUser, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.MarkPaid(context.Background(), b.ID); err != nil {
		t.Fatal(err)
	}

	stranger := newUser(t, tx, "Stranger")
	if _, err := svc.CompleteBooking(context.Background(), stranger, b.ID); !errors.Is(err, booking.ErrForbidden) {
		t.Fatalf("got %v, want ErrForbidden", err)
	}

	out, err := svc.CompleteBooking(context.Background(), talentUser, b.ID)
	if err != nil {
		t.Fatalf("CompleteBooking: %v", err)
	}
	if out.StatusCode != "completed" {
		t.Fatalf("status = %q, want completed", out.StatusCode)
	}

	if _, err := svc.CompleteBooking(context.Background(), organizer, b.ID); !errors.Is(err, booking.ErrNotPending) {
		t.Fatalf("re-completing: got %v, want ErrNotPending", err)
	}
}
