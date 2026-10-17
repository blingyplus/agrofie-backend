package organizerprofile_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/blingyplus/agrofie-backend/internal/organizerprofile"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed organizerprofile tests")
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

func newOrganizer(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	var id string
	err := tx.QueryRow(context.Background(), `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT 'org_' || gen_random_uuid() || '@example.test', id, gen_random_uuid() FROM user_statuses WHERE code = 'active'
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO profiles (user_id, display_name) VALUES ($1, 'Ama Org')`, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func ptr(s string) *string { return &s }

func TestGetCreatesEmptyProfile(t *testing.T) {
	tx := newTx(t)
	svc := organizerprofile.NewService(tx)
	uid := newOrganizer(t, tx)
	p, err := svc.Get(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Ama Org" || p.Organization != nil || p.HomePlaceCode != nil || len(p.EventTypes) != 0 {
		t.Fatalf("unexpected empty profile: %+v", p)
	}
}

func TestUpdateRoundTripAndPartial(t *testing.T) {
	tx := newTx(t)
	svc := organizerprofile.NewService(tx)
	ctx := context.Background()
	uid := newOrganizer(t, tx)

	p, err := svc.Update(ctx, uid, organizerprofile.UpdateInput{
		DisplayName: ptr("Ama Mensah"), Organization: ptr("Mensah Events"), Phone: ptr("+233 24 123 4567"),
		HomePlaceCode: ptr("greater_accra"), EventTypeCodes: []string{"wedding", "funeral", "wedding"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Ama Mensah" || *p.Organization != "Mensah Events" || *p.HomePlaceCode != "greater_accra" || len(p.EventTypes) != 2 {
		t.Fatalf("round trip: %+v", p)
	}

	// Partial: only the organisation changes; region, phone and event types stay.
	p, err = svc.Update(ctx, uid, organizerprofile.UpdateInput{Organization: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Organization != nil || p.HomePlaceCode == nil || p.Phone == nil || len(p.EventTypes) != 2 {
		t.Fatalf("partial update changed too much: %+v", p)
	}

	// Empty list clears event types; empty string clears the region.
	p, err = svc.Update(ctx, uid, organizerprofile.UpdateInput{EventTypeCodes: []string{}, HomePlaceCode: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.EventTypes) != 0 || p.HomePlaceCode != nil {
		t.Fatalf("clear: %+v", p)
	}
}

func TestUpdateValidation(t *testing.T) {
	tx := newTx(t)
	svc := organizerprofile.NewService(tx)
	ctx := context.Background()
	uid := newOrganizer(t, tx)
	if _, err := svc.Update(ctx, uid, organizerprofile.UpdateInput{HomePlaceCode: ptr("atlantis")}); !errors.Is(err, organizerprofile.ErrUnknownCode) {
		t.Fatalf("unknown region: %v", err)
	}
	if _, err := svc.Update(ctx, uid, organizerprofile.UpdateInput{EventTypeCodes: []string{"nope"}}); !errors.Is(err, organizerprofile.ErrUnknownCode) {
		t.Fatalf("unknown event type: %v", err)
	}
	if _, err := svc.Update(ctx, uid, organizerprofile.UpdateInput{Phone: ptr("abc")}); !errors.Is(err, organizerprofile.ErrInvalidInput) {
		t.Fatalf("bad phone: %v", err)
	}
	if _, err := svc.Update(ctx, uid, organizerprofile.UpdateInput{DisplayName: ptr("  ")}); !errors.Is(err, organizerprofile.ErrInvalidInput) {
		t.Fatalf("blank name: %v", err)
	}
}
