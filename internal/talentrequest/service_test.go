package talentrequest_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/talentrequest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func str(s string) *string { return &s }
func num(n int) *int       { return &n }

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed talentrequest tests")
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
		SELECT $1, id, gen_random_uuid() FROM user_statuses WHERE code = 'active' RETURNING id::text`,
		fmt.Sprintf("org_%d@example.test", time.Now().UnixNano())).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(context.Background(), `INSERT INTO profiles (user_id, display_name) VALUES ($1, 'Org')`, id); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return id
}

func TestCreateValidation(t *testing.T) {
	tx := newTx(t)
	svc := talentrequest.NewService(tx)
	org := newOrganizer(t, tx)
	cases := map[string]talentrequest.Input{
		"empty":        {},
		"bad date":     {EventDate: str("12/12/2030")},
		"past date":    {EventDate: str("2001-01-01")},
		"zero budget":  {Note: "x", BudgetPesewas: num(0)},
		"huge budget":  {Note: "x", BudgetPesewas: num(999_999_999)},
		"unknown code": {GenreCode: str("no_such_genre")},
	}
	for name, in := range cases {
		if _, err := svc.Create(context.Background(), org, in); !errors.Is(err, talentrequest.ErrInvalidInput) {
			t.Errorf("%s: want ErrInvalidInput, got %v", name, err)
		}
	}
}

func TestCreateListAndSetStatus(t *testing.T) {
	tx := newTx(t)
	svc := talentrequest.NewService(tx)
	org, other := newOrganizer(t, tx), newOrganizer(t, tx)
	date := time.Now().AddDate(0, 1, 0).Format("2006-01-02")

	r, err := svc.Create(context.Background(), org, talentrequest.Input{
		GenreCode: str("highlife"), PlaceCode: str("greater_accra"), EventDate: &date, BudgetPesewas: num(150050), Note: "  Need a band  ",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if r.StatusCode != "open" || r.Note != "Need a band" || r.BudgetPesewas == nil || *r.BudgetPesewas != 150050 || r.GenreCode == nil {
		t.Fatalf("unexpected request: %+v", r)
	}

	mine, _ := svc.ListMine(context.Background(), org)
	theirs, _ := svc.ListMine(context.Background(), other)
	if len(mine) != 1 || len(theirs) != 0 {
		t.Fatalf("ListMine isolation broken: mine=%d theirs=%d", len(mine), len(theirs))
	}

	if _, err := svc.SetStatus(context.Background(), r.ID, "bogus"); !errors.Is(err, talentrequest.ErrInvalidInput) {
		t.Errorf("bogus status: %v", err)
	}
	if _, err := svc.SetStatus(context.Background(), "00000000-0000-0000-0000-000000000000", "closed"); !errors.Is(err, talentrequest.ErrNotFound) {
		t.Errorf("missing id: %v", err)
	}
	got, err := svc.SetStatus(context.Background(), r.ID, "matched")
	if err != nil || got.StatusCode != "matched" {
		t.Fatalf("set status: %v %+v", err, got)
	}
	open := "open"
	all, _ := svc.ListForAdmin(context.Background(), &open)
	for _, x := range all {
		if x.ID == r.ID {
			t.Errorf("matched request still listed as open")
		}
	}
}
