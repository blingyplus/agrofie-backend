package discovery_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/discovery"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Integration tests run against a real Postgres (Compose) inside a transaction
// that is always rolled back. Set DATABASE_URL to run them; they skip otherwise.
// Every test creates its own genre and filters on it, so seeded data cannot leak in.

func newTx(t *testing.T) pgx.Tx {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set; skipping Postgres-backed discovery tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func newService(tx pgx.Tx) *discovery.Service { return discovery.NewService(db.New(tx)) }

func mustExec(t *testing.T, tx pgx.Tx, sql string, args ...any) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}

func mustScanString(t *testing.T, tx pgx.Tx, sql string, args ...any) string {
	t.Helper()
	var out string
	if err := tx.QueryRow(context.Background(), sql, args...).Scan(&out); err != nil {
		t.Fatalf("query %q: %v", sql, err)
	}
	return out
}

var seq int

func uniq(prefix string) string {
	seq++
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), seq)
}

func newGenre(t *testing.T, tx pgx.Tx) string {
	t.Helper()
	code := uniq("tgenre")
	mustExec(t, tx, `INSERT INTO genres (code, name) VALUES ($1, $1)`, code)
	return code
}

// newPlace inserts a place under GH. parent may be "" for a region.
func newPlace(t *testing.T, tx pgx.Tx, parent, level string) string {
	t.Helper()
	code := uniq("tplace")
	if parent == "" {
		mustExec(t, tx, `INSERT INTO geo_places (country_id, code, name, place_level)
			SELECT id, $1, $1, $2 FROM countries WHERE code = 'GH'`, code, level)
		return code
	}
	mustExec(t, tx, `INSERT INTO geo_places (country_id, parent_id, code, name, place_level)
		SELECT c.id, p.id, $1, $1, $3 FROM countries c
		JOIN geo_places p ON p.country_id = c.id AND p.code = $2
		WHERE c.code = 'GH'`, code, parent, level)
	return code
}

type rateFixture struct {
	Amount   string
	Unit     string // rate_units.code
	Expired  bool
	FromDays int // effective_from = now() - FromDays
}

type talentFixture struct {
	Name       string
	Searchable bool
	Status     string // user_statuses.code; default active
	Genres     []string
	Types      []string
	Languages  []string
	Home       string
	Areas      []string
	Rates      []rateFixture
	CreatedAt  time.Time
}

func newTalent(t *testing.T, tx pgx.Tx, f talentFixture) string {
	t.Helper()
	ctx := context.Background()
	if f.Status == "" {
		f.Status = "active"
	}
	if f.CreatedAt.IsZero() {
		f.CreatedAt = time.Now()
	}
	email := uniq("t") + "@example.test"
	userID := mustScanString(t, tx, `
		INSERT INTO users (email, user_status_id, kratos_identity_id)
		SELECT $1, id, gen_random_uuid() FROM user_statuses WHERE code = $2
		RETURNING id::text`, email, f.Status)
	mustExec(t, tx, `INSERT INTO profiles (user_id, display_name) VALUES ($1, $2)`, userID, f.Name)
	var homeArg any
	if f.Home != "" {
		homeArg = mustScanString(t, tx, `SELECT g.id::text FROM geo_places g JOIN countries c ON c.id=g.country_id
			WHERE c.code='GH' AND g.code=$1`, f.Home)
	}
	profileID := mustScanString(t, tx, `
		INSERT INTO talent_profiles (user_id, headline, bio, home_geo_place_id, is_searchable, created_at)
		VALUES ($1, $2, $3, $4::uuid, $5, $6) RETURNING id::text`,
		userID, "Headline "+f.Name, "Bio of "+f.Name, homeArg, f.Searchable, f.CreatedAt)
	for _, c := range f.Genres {
		mustExec(t, tx, `INSERT INTO talent_genres SELECT $1::uuid, id FROM genres WHERE code=$2`, profileID, c)
	}
	for _, c := range f.Types {
		mustExec(t, tx, `INSERT INTO talent_types_map SELECT $1::uuid, id FROM talent_types WHERE code=$2`, profileID, c)
	}
	for _, c := range f.Languages {
		mustExec(t, tx, `INSERT INTO talent_languages SELECT $1::uuid, id FROM languages WHERE code=$2`, profileID, c)
	}
	for _, c := range f.Areas {
		mustExec(t, tx, `INSERT INTO talent_service_areas SELECT $1::uuid, g.id FROM geo_places g
			JOIN countries co ON co.id=g.country_id WHERE co.code='GH' AND g.code=$2`, profileID, c)
	}
	for _, r := range f.Rates {
		to := any(nil)
		if r.Expired {
			to = time.Now().Add(-time.Hour)
		}
		mustExec(t, tx, `INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id, effective_from, effective_to)
			SELECT $1::uuid, $2::numeric, cur.id, ru.id, now() - make_interval(days => $3), $4::timestamptz
			FROM currencies cur, rate_units ru WHERE cur.code='GHS' AND ru.code=$5`,
			profileID, r.Amount, r.FromDays+1, to, r.Unit)
	}
	_ = ctx
	return profileID
}

func ids(page discovery.Page) []string {
	out := make([]string, 0, len(page.Items))
	for _, c := range page.Items {
		out = append(out, c.ID)
	}
	return out
}

func containsAll(got []string, want ...string) bool {
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}

func TestSearchExcludesHiddenAndSuspendedTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)

	visible := newTalent(t, tx, talentFixture{Name: "Visible", Searchable: true, Genres: []string{g}})
	hidden := newTalent(t, tx, talentFixture{Name: "Hidden", Searchable: false, Genres: []string{g}})
	suspended := newTalent(t, tx, talentFixture{Name: "Suspended", Searchable: true, Status: "suspended", Genres: []string{g}})

	page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g}}, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	got := ids(page)
	if len(got) != 1 || got[0] != visible {
		t.Fatalf("got %v, want only %s (hidden=%s suspended=%s)", got, visible, hidden, suspended)
	}
}

func TestSearchGenreFilterMatchesAnyOfTheGivenGenres(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g1, g2, g3 := newGenre(t, tx), newGenre(t, tx), newGenre(t, tx)
	a := newTalent(t, tx, talentFixture{Name: "A", Searchable: true, Genres: []string{g1}})
	b := newTalent(t, tx, talentFixture{Name: "B", Searchable: true, Genres: []string{g2}})
	newTalent(t, tx, talentFixture{Name: "C", Searchable: true, Genres: []string{g3}})

	page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g1, g2}}, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	got := ids(page)
	if len(got) != 2 || !containsAll(got, a, b) {
		t.Fatalf("got %v, want exactly %s and %s", got, a, b)
	}
}

func TestSearchCombinesDifferentFiltersWithAND(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	both := newTalent(t, tx, talentFixture{Name: "Both", Searchable: true, Genres: []string{g}, Types: []string{"dj"}, Languages: []string{"tw"}})
	newTalent(t, tx, talentFixture{Name: "GenreOnly", Searchable: true, Genres: []string{g}})
	newTalent(t, tx, talentFixture{Name: "WrongType", Searchable: true, Genres: []string{g}, Types: []string{"mc"}, Languages: []string{"tw"}})

	page, err := svc.Search(context.Background(), discovery.Filter{
		GenreCodes: []string{g}, TypeCodes: []string{"dj"}, LanguageCodes: []string{"tw"},
	}, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page); len(got) != 1 || got[0] != both {
		t.Fatalf("got %v, want only %s", got, both)
	}
}

func TestSearchRegionMatchesHomeDescendantsAndServiceAreas(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	region := newPlace(t, tx, "", "region")
	district := newPlace(t, tx, region, "district")
	otherRegion := newPlace(t, tx, "", "region")

	homeInDistrict := newTalent(t, tx, talentFixture{Name: "Home", Searchable: true, Genres: []string{g}, Home: district})
	servesRegion := newTalent(t, tx, talentFixture{Name: "Serves", Searchable: true, Genres: []string{g}, Home: otherRegion, Areas: []string{region}})
	newTalent(t, tx, talentFixture{Name: "Elsewhere", Searchable: true, Genres: []string{g}, Home: otherRegion})

	page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g}, PlaceCode: region}, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	got := ids(page)
	if len(got) != 2 || !containsAll(got, homeInDistrict, servesRegion) {
		t.Fatalf("got %v, want %s and %s", got, homeInDistrict, servesRegion)
	}
}

func TestSearchPaginatesNewestFirstWithoutGapsOrDuplicates(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	base := time.Now().Add(-time.Hour)
	var want []string // newest first
	for i := 0; i < 5; i++ {
		id := newTalent(t, tx, talentFixture{
			Name: fmt.Sprintf("T%d", i), Searchable: true, Genres: []string{g},
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		want = append([]string{id}, want...)
	}

	var got []string
	after := ""
	for pages := 0; pages < 10; pages++ {
		page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g}}, 2, after)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(page)...)
		if page.NextCursor == "" {
			break
		}
		after = page.NextCursor
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSearchRejectsMalformedCursor(t *testing.T) {
	tx := newTx(t)
	_, err := newService(tx).Search(context.Background(), discovery.Filter{}, 10, "not-a-cursor")
	if !errors.Is(err, discovery.ErrInvalidCursor) {
		t.Fatalf("got %v, want ErrInvalidCursor", err)
	}
}

func TestCardShowsCheapestCurrentRateAndIgnoresExpiredOnes(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	newTalent(t, tx, talentFixture{Name: "Priced", Searchable: true, Genres: []string{g}, Rates: []rateFixture{
		{Amount: "800.00", Unit: "per_event"},
		{Amount: "150.00", Unit: "per_hour"},
		{Amount: "50.00", Unit: "per_set", Expired: true},
	}})
	newTalent(t, tx, talentFixture{Name: "NoRate", Searchable: true, Genres: []string{g}})

	page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g}}, 20, "")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]discovery.Card{}
	for _, c := range page.Items {
		byName[c.DisplayName] = c
	}
	p := byName["Priced"]
	if p.FromRate == nil || p.FromRate.Amount != "150.00" || p.FromRate.CurrencyCode != "GHS" || p.FromRate.RateUnitCode != "per_hour" {
		t.Fatalf("Priced.FromRate = %+v, want 150.00 GHS per_hour", p.FromRate)
	}
	if byName["NoRate"].FromRate != nil {
		t.Fatalf("NoRate.FromRate = %+v, want nil", byName["NoRate"].FromRate)
	}
}

func TestCardCarriesGenreAndTypeTags(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	newTalent(t, tx, talentFixture{Name: "Tagged", Searchable: true, Genres: []string{g}, Types: []string{"dj"}})

	page, err := svc.Search(context.Background(), discovery.Filter{GenreCodes: []string{g}}, 20, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("err=%v items=%d", err, len(page.Items))
	}
	c := page.Items[0]
	if len(c.Genres) != 1 || c.Genres[0].Code != g {
		t.Fatalf("Genres = %+v, want [%s]", c.Genres, g)
	}
	if len(c.Types) != 1 || c.Types[0].Code != "dj" {
		t.Fatalf("Types = %+v, want [dj]", c.Types)
	}
}

func TestGetReturnsFullProfileForSearchableTalent(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	g := newGenre(t, tx)
	region := newPlace(t, tx, "", "region")
	id := newTalent(t, tx, talentFixture{
		Name: "Detail", Searchable: true, Genres: []string{g}, Languages: []string{"tw"},
		Home: region, Areas: []string{region}, Rates: []rateFixture{{Amount: "500.00", Unit: "per_event"}},
	})

	p, err := svc.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DisplayName != "Detail" || p.Bio == nil || *p.Bio != "Bio of Detail" {
		t.Fatalf("profile = %+v", p)
	}
	if len(p.Languages) != 1 || p.Languages[0].Code != "tw" {
		t.Fatalf("Languages = %+v", p.Languages)
	}
	if len(p.ServiceAreas) != 1 || p.ServiceAreas[0].Code != region {
		t.Fatalf("ServiceAreas = %+v", p.ServiceAreas)
	}
	if len(p.Rates) != 1 || p.Rates[0].Amount != "500.00" {
		t.Fatalf("Rates = %+v", p.Rates)
	}
}

func TestGetTreatsHiddenSuspendedAndUnknownTalentAsNotFound(t *testing.T) {
	tx := newTx(t)
	svc := newService(tx)
	hidden := newTalent(t, tx, talentFixture{Name: "Hidden", Searchable: false})
	suspended := newTalent(t, tx, talentFixture{Name: "Suspended", Searchable: true, Status: "suspended"})

	for name, id := range map[string]string{
		"hidden":    hidden,
		"suspended": suspended,
		"unknown":   "00000000-0000-0000-0000-000000000001",
	} {
		if _, err := svc.Get(context.Background(), id); !errors.Is(err, discovery.ErrNotFound) {
			t.Errorf("%s: got %v, want ErrNotFound", name, err)
		}
	}
	if _, err := svc.Get(context.Background(), "not-a-uuid"); !errors.Is(err, discovery.ErrNotFound) {
		t.Errorf("malformed id: got %v, want ErrNotFound", err)
	}
}

func TestDatabaseAllowsOnlyOneCurrentRatePerUnit(t *testing.T) {
	tx := newTx(t)
	id := newTalent(t, tx, talentFixture{Name: "Rates", Searchable: true, Rates: []rateFixture{{Amount: "100.00", Unit: "per_event"}}})

	_, err := tx.Exec(context.Background(), `INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id)
		SELECT $1::uuid, 200, cur.id, ru.id FROM currencies cur, rate_units ru WHERE cur.code='GHS' AND ru.code='per_event'`, id)
	if err == nil {
		t.Fatal("expected unique violation for a second current per_event rate")
	}
}
