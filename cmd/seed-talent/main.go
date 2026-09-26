// seed-talent creates demo talent through the real auth path (Kratos + marketplace
// provisioning), then fills in searchable profiles. Local development only.
// Safe to re-run: existing users are reused and their profile data is replaced.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/auth"
	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rate struct {
	Amount string
	Unit   string // rate_units.code
}

type act struct {
	Slug      string
	Name      string
	Headline  string
	Bio       string
	Home      string   // geo_places.code
	Areas     []string // geo_places.code
	Genres    []string
	Types     []string
	Languages []string
	Rates     []rate
}

var acts = []act{
	{"ama-serwaa", "Ama Serwaa & The Golden Keys", "Highlife band for weddings and galas", "Eight-piece highlife band. Horns, keys and a vocalist who knows every classic your guests will request.", "greater_accra", []string{"greater_accra", "central"}, []string{"highlife"}, []string{"musician"}, []string{"en", "tw", "ga"}, []rate{{"4500.00", "per_event"}}},
	{"kofi-mensimah", "DJ Kofi Mensimah", "Afrobeats and amapiano sets that fill the floor", "Ten years behind the decks at Accra clubs and private events. Brings full sound and lights on request.", "greater_accra", []string{"greater_accra", "eastern"}, []string{"afrobeats", "hiphop"}, []string{"dj"}, []string{"en", "tw"}, []rate{{"1800.00", "per_event"}, {"350.00", "per_hour"}}},
	{"nana-yaa", "Nana Yaa Adowa Troupe", "Traditional Akan drumming and dance", "Adowa and kete performance for funerals, durbars and cultural programmes. Costumes included.", "ashanti", []string{"ashanti", "bono_east"}, []string{"traditional"}, []string{"traditional_performer", "dancer"}, []string{"tw"}, []rate{{"2500.00", "per_event"}}},
	{"emmanuel-sax", "Emmanuel Quaye Sax", "Smooth jazz saxophone for dinners and receptions", "Solo or quartet. Quiet, polished background music that lets conversation breathe.", "greater_accra", []string{"greater_accra"}, []string{"jazz"}, []string{"musician"}, []string{"en"}, []rate{{"1200.00", "per_event"}, {"300.00", "per_hour"}}},
	{"abena-grace", "Abena Grace Worship Team", "Gospel worship led by a seasoned vocal team", "Church programmes, conferences and thanksgiving services. Own sound engineer available.", "ashanti", []string{"ashanti", "greater_accra"}, []string{"gospel"}, []string{"musician"}, []string{"en", "tw"}, []rate{{"2000.00", "per_event"}}},
	{"kwame-hiplife", "Kwame Blaze", "Hiplife and hip-hop live act", "High-energy live performance with a two-man backing crew. Festival and campus experience.", "ashanti", []string{"ashanti", "greater_accra"}, []string{"hiplife", "hiphop"}, []string{"musician"}, []string{"en", "tw"}, []rate{{"3000.00", "per_event"}}},
	{"efua-mc", "Efua Boateng MC", "Bilingual MC for weddings, launches and corporate nights", "Keeps the programme on time and the room warm in English, Twi and Fante.", "central", []string{"central", "greater_accra", "western"}, nil, []string{"mc"}, []string{"en", "tw"}, []rate{{"900.00", "per_event"}}},
	{"tamale-drums", "Tamale Dakpema Drummers", "Northern drumming and dance ensemble", "Traditional Dagbamba drumming for chieftaincy events, festivals and welcome ceremonies.", "northern", []string{"northern", "savannah", "upper_east"}, []string{"traditional"}, []string{"traditional_performer", "dancer"}, []string{"dag"}, []rate{{"2200.00", "per_event"}}},
	{"selorm-reggae", "Selorm & The Volta Vibes", "Reggae and highlife groove band", "Relaxed, danceable sets for beach parties, hotel residencies and private events.", "volta", []string{"volta", "greater_accra"}, []string{"reggae", "highlife"}, []string{"musician"}, []string{"ee", "en"}, []rate{{"2800.00", "per_event"}}},
	{"yaw-sound", "Yaw Opoku Sound", "Live sound engineering for bands and events", "PA, monitors and mixing for stages up to 500 people. Reliable setup and pack-down.", "greater_accra", []string{"greater_accra", "eastern", "central"}, nil, []string{"sound_engineer"}, []string{"en", "tw"}, []rate{{"1000.00", "per_event"}}},
	{"adjoa-dancers", "Adjoa Kente Dancers", "Contemporary and cultural dance crew", "Six dancers with choreography for corporate openings, weddings and music videos.", "greater_accra", []string{"greater_accra"}, []string{"afrobeats", "traditional"}, []string{"dancer"}, []string{"en", "ga"}, []rate{{"1500.00", "per_event"}}},
	{"kojo-newartist", "Kojo Arhin", "Up-and-coming afrobeats singer", "New voice with a growing following. Available for showcases and small events. No published rate yet.", "western", []string{"western"}, []string{"afrobeats"}, []string{"musician"}, []string{"en", "tw"}, nil},
}

func main() {
	cfg := config.Load("seed-talent")
	password := os.Getenv("SEED_TALENT_PASSWORD")
	if password == "" {
		slog.Error("SEED_TALENT_PASSWORD is required (see .env.example)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	svc := auth.NewService(pool, auth.NewKratos(cfg.KratosAdminURL, cfg.KratosPublicURL))
	for _, a := range acts {
		email := "seed." + a.Slug + "@agrofie.local"
		if _, err := svc.Register(ctx, auth.RegisterInput{
			Email: email, Password: password, DisplayName: a.Name, RoleCode: domain.RoleTalent,
		}); err != nil && !errors.Is(err, auth.ErrConflict) {
			slog.Error("register failed", "email", email, "err", err)
			os.Exit(1)
		}
		if err := fillProfile(ctx, pool, email, a); err != nil {
			slog.Error("profile failed", "email", email, "err", err)
			os.Exit(1)
		}
		fmt.Printf("seeded %s (%s)\n", a.Name, email)
	}
}

func fillProfile(ctx context.Context, pool *pgxpool.Pool, email string, a act) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var profileID string
	err = tx.QueryRow(ctx, `
		SELECT tp.id::text FROM talent_profiles tp JOIN users u ON u.id = tp.user_id WHERE u.email = $1`, email).Scan(&profileID)
	if err != nil {
		return fmt.Errorf("find talent profile: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE talent_profiles SET headline = $2, bio = $3, is_searchable = true, updated_at = now(),
			home_geo_place_id = (SELECT g.id FROM geo_places g JOIN countries c ON c.id = g.country_id WHERE c.code = 'GH' AND g.code = $4)
		WHERE id = $1::uuid`, profileID, a.Headline, a.Bio, a.Home); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}

	for _, t := range []string{"talent_genres", "talent_types_map", "talent_languages", "talent_service_areas"} {
		if _, err := tx.Exec(ctx, "DELETE FROM "+t+" WHERE talent_profile_id = $1::uuid", profileID); err != nil {
			return fmt.Errorf("clear %s: %w", t, err)
		}
	}
	insert := func(sql string, codes []string) error {
		for _, c := range codes {
			tag, err := tx.Exec(ctx, sql, profileID, c)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("unknown lookup code %q", c)
			}
		}
		return nil
	}
	if err := insert(`INSERT INTO talent_genres SELECT $1::uuid, id FROM genres WHERE code = $2`, a.Genres); err != nil {
		return fmt.Errorf("genres: %w", err)
	}
	if err := insert(`INSERT INTO talent_types_map SELECT $1::uuid, id FROM talent_types WHERE code = $2`, a.Types); err != nil {
		return fmt.Errorf("types: %w", err)
	}
	if err := insert(`INSERT INTO talent_languages SELECT $1::uuid, id FROM languages WHERE code = $2`, a.Languages); err != nil {
		return fmt.Errorf("languages: %w", err)
	}
	if err := insert(`INSERT INTO talent_service_areas SELECT $1::uuid, g.id FROM geo_places g
		JOIN countries c ON c.id = g.country_id WHERE c.code = 'GH' AND g.code = $2`, a.Areas); err != nil {
		return fmt.Errorf("service areas: %w", err)
	}

	// Replace current rates: close open ones, then insert the seed rates.
	if _, err := tx.Exec(ctx, `UPDATE talent_rates SET effective_to = now() WHERE talent_profile_id = $1::uuid AND effective_to IS NULL`, profileID); err != nil {
		return fmt.Errorf("close rates: %w", err)
	}
	for _, r := range a.Rates {
		tag, err := tx.Exec(ctx, `
			INSERT INTO talent_rates (talent_profile_id, amount, currency_id, rate_unit_id, effective_from)
			SELECT $1::uuid, $2::numeric, cur.id, ru.id, now() - interval '1 minute'
			FROM currencies cur, rate_units ru WHERE cur.code = 'GHS' AND ru.code = $3`, profileID, r.Amount, r.Unit)
		if err != nil {
			return fmt.Errorf("insert rate: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("unknown rate unit %q", r.Unit)
		}
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	return nil
}
