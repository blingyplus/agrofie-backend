package lookup_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-backed, like the other internal/* services: real transaction,
// always rolled back.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed lookup tests")
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

// newGenre inserts a fresh, isolated genre row (rather than depending on
// seed data content, which is free to change) so tests own the fixture
// they assert against.
func newGenre(t *testing.T, tx pgx.Tx, isActive bool) string {
	t.Helper()
	code := uniq("genre")
	if _, err := tx.Exec(context.Background(), `
		INSERT INTO genres (code, name, sort_order, is_active) VALUES ($1, $2, 0, $3)`,
		code, "Original name", isActive); err != nil {
		t.Fatalf("insert genre: %v", err)
	}
	return code
}

func TestGenresListsOnlyActiveByDefault(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	activeCode := newGenre(t, tx, true)
	inactiveCode := newGenre(t, tx, false)

	activeOnly, err := svc.Genres(ctx, true)
	if err != nil {
		t.Fatalf("Genres(activeOnly=true): %v", err)
	}
	if !containsCode(activeOnly, activeCode) {
		t.Fatalf("expected active genre %s in %+v", activeCode, activeOnly)
	}
	if containsCode(activeOnly, inactiveCode) {
		t.Fatalf("did not expect inactive genre %s in %+v", inactiveCode, activeOnly)
	}

	all, err := svc.Genres(ctx, false)
	if err != nil {
		t.Fatalf("Genres(activeOnly=false): %v", err)
	}
	if !containsCode(all, inactiveCode) {
		t.Fatalf("expected inactive genre %s when activeOnly=false, got %+v", inactiveCode, all)
	}
}

func TestUpdateNameRenamesWithoutChangingCode(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))
	code := newGenre(t, tx, true)

	updated, err := svc.UpdateName(ctx, lookup.TableGenres, code, "Renamed")
	if err != nil {
		t.Fatalf("UpdateName: %v", err)
	}
	if updated.Code != code {
		t.Fatalf("code changed: got %q, want %q", updated.Code, code)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("name = %q, want %q", updated.Name, "Renamed")
	}

	rows, err := svc.Genres(ctx, true)
	if err != nil {
		t.Fatalf("Genres: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Code == code {
			found = true
			if r.Name != "Renamed" {
				t.Fatalf("listed name = %q, want %q", r.Name, "Renamed")
			}
		}
	}
	if !found {
		t.Fatalf("renamed genre %s not found in listing", code)
	}
}

func TestSetActiveTogglesVisibility(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))
	code := newGenre(t, tx, true)

	if _, err := svc.SetActive(ctx, lookup.TableGenres, code, false); err != nil {
		t.Fatalf("SetActive(false): %v", err)
	}
	activeOnly, err := svc.Genres(ctx, true)
	if err != nil {
		t.Fatalf("Genres: %v", err)
	}
	if containsCode(activeOnly, code) {
		t.Fatalf("deactivated genre %s should not appear with activeOnly=true", code)
	}

	if _, err := svc.SetActive(ctx, lookup.TableGenres, code, true); err != nil {
		t.Fatalf("SetActive(true): %v", err)
	}
	activeOnly, err = svc.Genres(ctx, true)
	if err != nil {
		t.Fatalf("Genres: %v", err)
	}
	if !containsCode(activeOnly, code) {
		t.Fatalf("reactivated genre %s should appear with activeOnly=true", code)
	}
}

func TestUpdateNameRejectsUnknownCode(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	if _, err := svc.UpdateName(ctx, lookup.TableGenres, "does_not_exist", "New name"); !errors.Is(err, lookup.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestSetActiveRejectsUnknownCode(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	if _, err := svc.SetActive(ctx, lookup.TableGenres, "does_not_exist", false); !errors.Is(err, lookup.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestUpdateNameRejectsUnknownTable(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	if _, err := svc.UpdateName(ctx, lookup.Table("BOGUS_TABLE"), "any", "New name"); !errors.Is(err, lookup.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

func TestSetActiveRejectsUnknownTable(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	if _, err := svc.SetActive(ctx, lookup.Table("BOGUS_TABLE"), "any", false); !errors.Is(err, lookup.ErrInvalidInput) {
		t.Fatalf("got %v, want ErrInvalidInput", err)
	}
}

// newGeoPlace inserts a country/region/city chain to test GeoPlaces'
// country + parent filtering in isolation from seed data.
func newGeoPlace(t *testing.T, tx pgx.Tx, countryCode string, parentID *string, placeLevel string) (id, code string) {
	t.Helper()
	code = uniq("place")
	var countryID string
	if err := tx.QueryRow(context.Background(), `SELECT id::text FROM countries WHERE code = $1`, countryCode).Scan(&countryID); err != nil {
		t.Fatalf("lookup country %s: %v", countryCode, err)
	}
	if err := tx.QueryRow(context.Background(), `
		INSERT INTO geo_places (country_id, parent_id, code, name, place_level, sort_order, is_active)
		VALUES ($1, $2, $3, $4, $5, 0, true) RETURNING id::text`,
		countryID, parentID, code, "Test place", placeLevel).Scan(&id); err != nil {
		t.Fatalf("insert geo_place: %v", err)
	}
	return id, code
}

func TestGeoPlacesFiltersByCountryAndParent(t *testing.T) {
	tx := newTx(t)
	ctx := context.Background()
	svc := lookup.NewService(db.New(tx))

	regionID, regionCode := newGeoPlace(t, tx, "GH", nil, "region")
	_, cityCode := newGeoPlace(t, tx, "GH", &regionID, "city")

	topLevel, err := svc.GeoPlaces(ctx, "GH", nil, true)
	if err != nil {
		t.Fatalf("GeoPlaces(top-level): %v", err)
	}
	if !containsGeoCode(topLevel, regionCode) {
		t.Fatalf("expected top-level region %s in %+v", regionCode, topLevel)
	}
	if containsGeoCode(topLevel, cityCode) {
		t.Fatalf("did not expect child city %s at top level", cityCode)
	}

	children, err := svc.GeoPlaces(ctx, "GH", &regionCode, true)
	if err != nil {
		t.Fatalf("GeoPlaces(children of %s): %v", regionCode, err)
	}
	if !containsGeoCode(children, cityCode) {
		t.Fatalf("expected child city %s under region %s, got %+v", cityCode, regionCode, children)
	}
	if children[0].ParentCode == nil || *children[0].ParentCode != regionCode {
		t.Fatalf("child parentCode = %v, want %s", children[0].ParentCode, regionCode)
	}
}

func containsCode(items []lookup.Item, code string) bool {
	for _, i := range items {
		if i.Code == code {
			return true
		}
	}
	return false
}

func containsGeoCode(items []lookup.GeoPlace, code string) bool {
	for _, i := range items {
		if i.Code == code {
			return true
		}
	}
	return false
}
