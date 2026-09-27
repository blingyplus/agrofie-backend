package verification_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/verification"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like internal/discovery and internal/talentprofile: real
// transaction, always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed verification tests")
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

func newTalentUser(t *testing.T, tx pgx.Tx, name string) string {
	t.Helper()
	userID := newUser(t, tx, name)
	if _, err := tx.Exec(context.Background(), `INSERT INTO talent_profiles (user_id) VALUES ($1)`, userID); err != nil {
		t.Fatalf("insert talent_profiles: %v", err)
	}
	return userID
}

func isSearchable(t *testing.T, tx pgx.Tx, userID string) bool {
	t.Helper()
	var v bool
	if err := tx.QueryRow(context.Background(), `SELECT is_searchable FROM talent_profiles WHERE user_id = $1`, userID).Scan(&v); err != nil {
		t.Fatalf("read is_searchable: %v", err)
	}
	return v
}

func newService(tx pgx.Tx) *verification.Service { return verification.NewService(tx) }

func TestSubmitRejectsUnknownTypeCode(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	userID := newTalentUser(t, tx, "Talent")

	_, err := svc.Submit(context.Background(), userID, verification.SubmitInput{TypeCode: "not-a-real-type"})
	if !errors.Is(err, verification.ErrUnknownCode) {
		t.Fatalf("got %v, want ErrUnknownCode", err)
	}
}

func TestSubmitCreatesAPendingVerification(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	userID := newTalentUser(t, tx, "Talent")
	ref := "https://example.test/id-photo.jpg"

	v, err := svc.Submit(context.Background(), userID, verification.SubmitInput{TypeCode: "identity", EvidenceRef: &ref})
	if err != nil {
		t.Fatal(err)
	}
	if v.StatusCode != "pending" || v.TypeCode != "identity" {
		t.Fatalf("got %+v", v)
	}
}

func TestListPendingShowsOnlyPendingAcrossTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	a := newTalentUser(t, tx, "Talent A")
	b := newTalentUser(t, tx, "Talent B")

	va, err := svc.Submit(context.Background(), a, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	vb, err := svc.Submit(context.Background(), b, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), admin, va.ID, nil); err != nil {
		t.Fatal(err)
	}

	pending, err := svc.ListPending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != vb.ID {
		t.Fatalf("got %+v, want only %s pending", pending, vb.ID)
	}
}

func TestApproveIdentityVerificationMakesTalentSearchable(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	talent := newTalentUser(t, tx, "Talent")
	v, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if isSearchable(t, tx, talent) {
		t.Fatal("should not be searchable before approval")
	}

	note := "ID checked against Ghana Card photo"
	out, err := svc.Approve(context.Background(), admin, v.ID, &note)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != "verified" {
		t.Fatalf("status = %q, want verified", out.StatusCode)
	}
	if !isSearchable(t, tx, talent) {
		t.Fatal("talent should be searchable after identity approval")
	}
}

func TestApproveNonIdentityVerificationDoesNotChangeSearchability(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	talent := newTalentUser(t, tx, "Talent")
	v, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "media"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), admin, v.ID, nil); err != nil {
		t.Fatal(err)
	}
	if isSearchable(t, tx, talent) {
		t.Fatal("a media approval alone should not make talent searchable")
	}
}

func TestRejectDoesNotChangeSearchability(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	talent := newTalentUser(t, tx, "Talent")
	v, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	reason := "Photo unreadable"
	out, err := svc.Reject(context.Background(), admin, v.ID, &reason)
	if err != nil {
		t.Fatal(err)
	}
	if out.StatusCode != "rejected" {
		t.Fatalf("status = %q, want rejected", out.StatusCode)
	}
	if isSearchable(t, tx, talent) {
		t.Fatal("rejection must not flip searchability")
	}
}

func TestApprovingAlreadyDecidedVerificationFails(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	talent := newTalentUser(t, tx, "Talent")
	v, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), admin, v.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), admin, v.ID, nil); !errors.Is(err, verification.ErrNotPending) {
		t.Fatalf("re-approving got %v, want ErrNotPending", err)
	}
	if _, err := svc.Reject(context.Background(), admin, v.ID, nil); !errors.Is(err, verification.ErrNotPending) {
		t.Fatalf("rejecting an approved one got %v, want ErrNotPending", err)
	}
}

func TestReviewingUnknownVerificationIDFails(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	_, err := svc.Approve(context.Background(), admin, "00000000-0000-0000-0000-000000000001", nil)
	if !errors.Is(err, verification.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestListMineShowsOwnHistoryNewestFirst(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	admin := newUser(t, tx, "Admin")
	talent := newTalentUser(t, tx, "Talent")
	other := newTalentUser(t, tx, "Other")

	v1, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reject(context.Background(), admin, v1.ID, nil); err != nil {
		t.Fatal(err)
	}
	v2, err := svc.Submit(context.Background(), talent, verification.SubmitInput{TypeCode: "identity"})
	if err != nil {
		t.Fatal(err)
	}
	// All Submit/Reject calls above run in nested savepoints of this test's
	// single outer transaction, so Postgres's now() resolves to the same
	// instant for every one of them (now() is transaction-start time, not
	// statement time) — set created_at explicitly so ordering is deterministic.
	if _, err := tx.Exec(context.Background(), `UPDATE verifications SET created_at = now() - interval '1 minute' WHERE id = $1::uuid`, v1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `UPDATE verifications SET created_at = now() WHERE id = $1::uuid`, v2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Submit(context.Background(), other, verification.SubmitInput{TypeCode: "identity"}); err != nil {
		t.Fatal(err)
	}

	mine, err := svc.ListMine(context.Background(), talent)
	if err != nil {
		t.Fatal(err)
	}
	if len(mine) != 2 || mine[0].ID != v2.ID || mine[1].ID != v1.ID {
		t.Fatalf("got %+v, want [%s(newest), %s]", mine, v2.ID, v1.ID)
	}
}
