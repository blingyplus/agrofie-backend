package availability_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like internal/discovery, internal/talentprofile and
// internal/verification: real transaction, always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed availability tests")
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
	var profileID string
	if err := tx.QueryRow(context.Background(), `INSERT INTO talent_profiles (user_id) VALUES ($1) RETURNING id::text`, userID).Scan(&profileID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	return userID
}

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

func talentProfileID(t *testing.T, tx pgx.Tx, userID string) string {
	t.Helper()
	var id string
	if err := tx.QueryRow(context.Background(), `SELECT id::text FROM talent_profiles WHERE user_id = $1`, userID).Scan(&id); err != nil {
		t.Fatalf("get talent profile id: %v", err)
	}
	return id
}

func newService(tx pgx.Tx) *availability.Service { return availability.NewService(tx) }

func day(offset int) time.Time {
	return time.Date(2026, time.November, 1+offset, 0, 0, 0, 0, time.UTC)
}

func TestAddBlockRejectsEndBeforeStart(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)

	_, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{
		StartsAt: day(5), EndsAt: day(2), IsAvailable: true,
	})
	if !errors.Is(err, availability.ErrInvalidRange) {
		t.Fatalf("got %v, want ErrInvalidRange", err)
	}
}

func TestAddBlockRejectsCallerWithNoTalentProfile(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	organizer := newOrganizerUser(t, tx)

	_, err := svc.AddBlock(context.Background(), organizer, availability.AddBlockInput{
		StartsAt: day(1), EndsAt: day(2), IsAvailable: true,
	})
	if !errors.Is(err, availability.ErrNotTalent) {
		t.Fatalf("got %v, want ErrNotTalent", err)
	}
}

func TestAddAndListMyBlocks(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)

	note := "Wedding season, open"
	block, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{
		StartsAt: day(1), EndsAt: day(5), IsAvailable: true, Note: &note,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !block.IsAvailable || block.Note == nil || *block.Note != note {
		t.Fatalf("got %+v", block)
	}

	blocks, err := svc.ListMine(context.Background(), talent, day(0), day(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].ID != block.ID {
		t.Fatalf("got %+v", blocks)
	}
}

func TestListMineOnlyReturnsBlocksOverlappingTheRequestedRange(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)

	inRange, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{StartsAt: day(3), EndsAt: day(4), IsAvailable: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.AddBlock(context.Background(), talent, availability.AddBlockInput{StartsAt: day(20), EndsAt: day(21), IsAvailable: true})
	if err != nil {
		t.Fatal(err)
	}

	blocks, err := svc.ListMine(context.Background(), talent, day(0), day(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].ID != inRange.ID {
		t.Fatalf("got %+v, want only the block inside the requested range", blocks)
	}
}

func TestRemoveBlockDeletesOwnBlock(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)
	block, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{StartsAt: day(1), EndsAt: day(2), IsAvailable: true})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RemoveBlock(context.Background(), talent, block.ID); err != nil {
		t.Fatal(err)
	}
	blocks, err := svc.ListMine(context.Background(), talent, day(0), day(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 0 {
		t.Fatalf("got %+v, want empty after removal", blocks)
	}
}

func TestRemoveBlockCannotDeleteAnotherTalentsBlock(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	owner := newTalentUser(t, tx)
	other := newTalentUser(t, tx)
	block, err := svc.AddBlock(context.Background(), owner, availability.AddBlockInput{StartsAt: day(1), EndsAt: day(2), IsAvailable: true})
	if err != nil {
		t.Fatal(err)
	}

	err = svc.RemoveBlock(context.Background(), other, block.ID)
	if !errors.Is(err, availability.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	blocks, listErr := svc.ListMine(context.Background(), owner, day(0), day(10))
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(blocks) != 1 {
		t.Fatalf("owner's block should be untouched, got %+v", blocks)
	}
}

func TestListForTalentNeverExposesNotes(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)
	profileID := talentProfileID(t, tx, talent)
	note := "Family emergency"
	_, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{
		StartsAt: day(1), EndsAt: day(2), IsAvailable: false, Note: &note,
	})
	if err != nil {
		t.Fatal(err)
	}

	windows, err := svc.ListForTalent(context.Background(), profileID, day(0), day(10))
	if err != nil {
		t.Fatal(err)
	}
	if len(windows) != 1 || windows[0].IsAvailable {
		t.Fatalf("got %+v", windows)
	}
}

func TestIsRangeAvailableRequiresFullCoverageByAnAvailableBlockAndNoBlackout(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	talent := newTalentUser(t, tx)
	profileID := talentProfileID(t, tx, talent)

	// Nothing published yet: not available.
	ok, err := svc.IsRangeAvailable(context.Background(), profileID, day(1), day(2))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected unavailable with no published blocks")
	}

	if _, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{StartsAt: day(0), EndsAt: day(10), IsAvailable: true}); err != nil {
		t.Fatal(err)
	}
	ok, err = svc.IsRangeAvailable(context.Background(), profileID, day(1), day(2))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected available once covered by an available block")
	}

	// A blackout inside the available window wins.
	if _, err := svc.AddBlock(context.Background(), talent, availability.AddBlockInput{StartsAt: day(1), EndsAt: day(2), IsAvailable: false}); err != nil {
		t.Fatal(err)
	}
	ok, err = svc.IsRangeAvailable(context.Background(), profileID, day(1), day(2))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected unavailable: a blackout block overlaps the requested range")
	}

	// A different, non-overlapping part of the available window is unaffected.
	ok, err = svc.IsRangeAvailable(context.Background(), profileID, day(5), day(6))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected available: outside the blackout, still inside the available window")
	}
}
