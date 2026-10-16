// seed-talent creates demo talent through the real auth path (Kratos + marketplace
// provisioning), then fills in searchable profiles. Local development only.
// Safe to re-run: existing users are reused and their profile data is replaced.
//
// Discover data: a few acts are flagged featured, joined dates are staggered
// (some "new"), and each act gets a set of completed bookings with organizer
// reviews so ratings and review counts vary (some acts have none).
//
// What goes through real code paths: users (auth.Register -> Kratos), payout
// subaccounts (talentprofile.ConnectPayout, fake provider) and every review
// (review.Service.Submit enforces completed-booking + party + one-per-side).
// What deliberately bypasses the booking service (documented, demo only):
//   - past, already-"completed" bookings are inserted with SQL (the real
//     request->accept->pay->complete flow cannot create bookings in the past),
//     tagged venue_text = 'demo seed', with no payment/contract/status-event rows;
//   - talent_profiles.created_at / reviews.created_at are back-dated.
//
// Re-running deletes and recreates only the bookings tagged 'demo seed'
// (their reviews cascade), so ratings stay deterministic.
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
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/blingyplus/agrofie-backend/internal/review"
	"github.com/blingyplus/agrofie-backend/internal/talentprofile"
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

	FeaturedRank int   // >0 = featured, lower first
	JoinedDays   int   // profile created this many days ago
	Ratings      []int // organizer review ratings (1-5); empty = no reviews yet
}

var acts = []act{
	{"ama-serwaa", "Ama Serwaa & The Golden Keys", "Highlife band for weddings and galas", "Eight-piece highlife band. Horns, keys and a vocalist who knows every classic your guests will request.", "greater_accra", []string{"greater_accra", "central"}, []string{"highlife"}, []string{"musician"}, []string{"en", "tw", "ga"}, []rate{{"4500.00", "per_event"}}, 1, 120, []int{5, 5, 5, 4, 5, 5, 5, 4, 5}},
	{"kofi-mensimah", "DJ Kofi Mensimah", "Afrobeats and amapiano sets that fill the floor", "Ten years behind the decks at Accra clubs and private events. Brings full sound and lights on request.", "greater_accra", []string{"greater_accra", "eastern"}, []string{"afrobeats", "hiphop"}, []string{"dj"}, []string{"en", "tw"}, []rate{{"1800.00", "per_event"}, {"350.00", "per_hour"}}, 2, 90, []int{5, 4, 5, 5, 4, 5, 5, 5, 5, 4, 5, 5}},
	{"nana-yaa", "Nana Yaa Adowa Troupe", "Traditional Akan drumming and dance", "Adowa and kete performance for funerals, durbars and cultural programmes. Costumes included.", "ashanti", []string{"ashanti", "bono_east"}, []string{"traditional"}, []string{"traditional_performer", "dancer"}, []string{"tw"}, []rate{{"2500.00", "per_event"}}, 0, 75, []int{5, 5, 4, 5}},
	{"emmanuel-sax", "Emmanuel Quaye Sax", "Smooth jazz saxophone for dinners and receptions", "Solo or quartet. Quiet, polished background music that lets conversation breathe.", "greater_accra", []string{"greater_accra"}, []string{"jazz"}, []string{"musician"}, []string{"en"}, []rate{{"1200.00", "per_event"}, {"300.00", "per_hour"}}, 0, 60, []int{4, 4, 5, 4, 4, 3}},
	{"abena-grace", "Abena Grace Worship Team", "Gospel worship led by a seasoned vocal team", "Church programmes, conferences and thanksgiving services. Own sound engineer available.", "ashanti", []string{"ashanti", "greater_accra"}, []string{"gospel"}, []string{"musician"}, []string{"en", "tw"}, []rate{{"2000.00", "per_event"}}, 3, 100, []int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 4, 5}},
	{"kwame-hiplife", "Kwame Blaze", "Hiplife and hip-hop live act", "High-energy live performance with a two-man backing crew. Festival and campus experience.", "ashanti", []string{"ashanti", "greater_accra"}, []string{"hiplife", "hiphop"}, []string{"musician"}, []string{"en", "tw"}, []rate{{"3000.00", "per_event"}}, 0, 45, []int{4, 3, 4, 4, 5, 3, 4}},
	{"efua-mc", "Efua Boateng MC", "Bilingual MC for weddings, launches and corporate nights", "Keeps the programme on time and the room warm in English, Twi and Fante.", "central", []string{"central", "greater_accra", "western"}, nil, []string{"mc"}, []string{"en", "tw"}, []rate{{"900.00", "per_event"}}, 0, 30, []int{5, 4, 5}},
	{"tamale-drums", "Tamale Dakpema Drummers", "Northern drumming and dance ensemble", "Traditional Dagbamba drumming for chieftaincy events, festivals and welcome ceremonies.", "northern", []string{"northern", "savannah", "upper_east"}, []string{"traditional"}, []string{"traditional_performer", "dancer"}, []string{"dag"}, []rate{{"2200.00", "per_event"}}, 4, 20, []int{4, 5}},
	{"selorm-reggae", "Selorm & The Volta Vibes", "Reggae and highlife groove band", "Relaxed, danceable sets for beach parties, hotel residencies and private events.", "volta", []string{"volta", "greater_accra"}, []string{"reggae", "highlife"}, []string{"musician"}, []string{"ee", "en"}, []rate{{"2800.00", "per_event"}}, 0, 12, []int{4, 4, 5, 4, 3}},
	{"yaw-sound", "Yaw Opoku Sound", "Live sound engineering for bands and events", "PA, monitors and mixing for stages up to 500 people. Reliable setup and pack-down.", "greater_accra", []string{"greater_accra", "eastern", "central"}, nil, []string{"sound_engineer"}, []string{"en", "tw"}, []rate{{"1000.00", "per_event"}}, 0, 9, []int{4, 4, 3, 4, 4}},
	{"adjoa-dancers", "Adjoa Kente Dancers", "Contemporary and cultural dance crew", "Six dancers with choreography for corporate openings, weddings and music videos.", "greater_accra", []string{"greater_accra"}, []string{"afrobeats", "traditional"}, []string{"dancer"}, []string{"en", "ga"}, []rate{{"1500.00", "per_event"}}, 0, 5, []int{}},
	{"kojo-newartist", "Kojo Arhin", "Up-and-coming afrobeats singer", "New voice with a growing following. Available for showcases and small events. No published rate yet.", "western", []string{"western"}, []string{"afrobeats"}, []string{"musician"}, []string{"en", "tw"}, nil, 0, 2, []int{}},
}

func main() {
	cfg := config.Load("seed-talent")
	// Demo/test value only (documented in .env.example); override via env.
	password := envOr("SEED_TALENT_PASSWORD", "changeme-seed-talent")
	orgPassword := envOr("SEED_ORGANIZER_PASSWORD", "changeme-seed-organizer")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	svc := auth.NewService(pool, auth.NewKratos(cfg.KratosAdminURL, cfg.KratosPublicURL))
	payouts := talentprofile.NewService(pool, payments.FakeProvider{}, cfg.CommissionPercent)
	for i, a := range acts {
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
		var userID string
		if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email = $1`, email).Scan(&userID); err != nil {
			slog.Error("find user failed", "email", email, "err", err)
			os.Exit(1)
		}
		// Fake (test) payout subaccount so the talent is bookable. Safe to repeat.
		if _, err := payouts.ConnectPayout(ctx, userID, talentprofile.ConnectPayoutInput{
			BusinessName: a.Name, SettlementType: payments.SettlementMobileMoney,
			BankCode: "MTN", AccountNumber: fmt.Sprintf("055000%04d", i+1),
		}); err != nil {
			slog.Error("payout failed", "email", email, "err", err)
			os.Exit(1)
		}
		fmt.Printf("seeded %s (%s)\n", a.Name, email)
	}

	orgs := []struct{ Email, Name string }{
		{"demo.organizer@agrofie.local", "Demo Organizer"},
		{"seed.esi.events@agrofie.local", "Esi Mensah Events"},
		{"seed.accra.weddings@agrofie.local", "Accra Weddings Co"},
		{"seed.kumasi.corporate@agrofie.local", "Kumasi Corporate Events"},
	}
	for _, o := range orgs {
		if _, err := svc.Register(ctx, auth.RegisterInput{
			Email: o.Email, Password: orgPassword, DisplayName: o.Name, RoleCode: domain.RoleOrganizer,
		}); err != nil && !errors.Is(err, auth.ErrConflict) {
			slog.Error("register organizer failed", "email", o.Email, "err", err)
			os.Exit(1)
		}
		fmt.Printf("seeded organizer %s\n", o.Email)
	}

	if err := seedReviews(ctx, pool); err != nil {
		slog.Error("seed reviews failed", "err", err)
		os.Exit(1)
	}
}

var reviewBodies = map[int][]string{
	5: {"Absolutely brilliant. Guests are still talking about it.", "On time, professional and the crowd loved them.", "Worth every cedi. Would book again.", "Exceeded expectations from start to finish."},
	4: {"Great performance, small delay setting up.", "Very good. Communication could be a bit quicker.", "Solid act and good energy."},
	3: {"Decent, but started late and the set felt short."},
}

// seedReviews replaces the demo completed bookings and their organizer reviews.
func seedReviews(ctx context.Context, pool *pgxpool.Pool) error {
	reviews := review.NewService(pool)
	if _, err := pool.Exec(ctx, `DELETE FROM bookings WHERE venue_text = 'demo seed'`); err != nil {
		return fmt.Errorf("clear demo bookings: %w", err)
	}
	orgEmails := []string{"demo.organizer@agrofie.local", "seed.esi.events@agrofie.local", "seed.accra.weddings@agrofie.local", "seed.kumasi.corporate@agrofie.local"}
	orgIDs := make([]string, len(orgEmails))
	for i, e := range orgEmails {
		if err := pool.QueryRow(ctx, `SELECT id::text FROM users WHERE email = $1`, e).Scan(&orgIDs[i]); err != nil {
			return fmt.Errorf("find organizer %s: %w", e, err)
		}
	}
	total := 0
	for ai, a := range acts {
		var profileID string
		if err := pool.QueryRow(ctx, `SELECT tp.id::text FROM talent_profiles tp JOIN users u ON u.id = tp.user_id WHERE u.email = $1`,
			"seed."+a.Slug+"@agrofie.local").Scan(&profileID); err != nil {
			return fmt.Errorf("find profile %s: %w", a.Slug, err)
		}
		for ri, rating := range a.Ratings {
			org := orgIDs[(ai+ri)%len(orgIDs)]
			daysAgo := 3 + ri*9 + ai%5 // spread over the past months
			var bookingID string
			err := pool.QueryRow(ctx, `
				INSERT INTO bookings (organizer_user_id, talent_profile_id, booking_status_id, starts_at, ends_at,
				                      venue_text, quoted_amount, currency_code, created_at, updated_at)
				SELECT $1::uuid, $2::uuid, bs.id, now() - make_interval(days => $3) + interval '18 hours',
				       now() - make_interval(days => $3) + interval '23 hours', 'demo seed',
				       (SELECT r.amount FROM talent_rates r WHERE r.talent_profile_id = $2::uuid AND r.effective_to IS NULL ORDER BY r.amount LIMIT 1),
				       'GHS', now() - make_interval(days => $3 + 14), now() - make_interval(days => $3)
				FROM booking_statuses bs WHERE bs.code = 'completed'
				RETURNING id::text`, org, profileID, daysAgo).Scan(&bookingID)
			if err != nil {
				return fmt.Errorf("insert booking: %w", err)
			}
			opts := reviewBodies[rating]
			var body *string
			if len(opts) > 0 {
				b := opts[(ai+ri)%len(opts)]
				body = &b
			}
			if _, err := reviews.Submit(ctx, org, bookingID, rating, body); err != nil {
				return fmt.Errorf("submit review: %w", err)
			}
			if _, err := pool.Exec(ctx, `UPDATE reviews SET created_at = now() - make_interval(days => $2) WHERE booking_id = $1::uuid`, bookingID, daysAgo-1); err != nil {
				return fmt.Errorf("backdate review: %w", err)
			}
			total++
		}
	}
	fmt.Printf("seeded %d completed bookings with organizer reviews\n", total)
	return nil
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
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
			featured = $5, featured_rank = NULLIF($6, 0), created_at = now() - make_interval(days => $7),
			home_geo_place_id = (SELECT g.id FROM geo_places g JOIN countries c ON c.id = g.country_id WHERE c.code = 'GH' AND g.code = $4)
		WHERE id = $1::uuid`, profileID, a.Headline, a.Bio, a.Home, a.FeaturedRank > 0, a.FeaturedRank, a.JoinedDays); err != nil {
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

	// Verified identity (what an admin approval does) + wide availability.
	if _, err := tx.Exec(ctx, `
		INSERT INTO verifications (user_id, verification_type_id, verification_status_id, notes)
		SELECT tp.user_id, vt.id, vs.id, 'demo seed'
		FROM talent_profiles tp, verification_types vt, verification_statuses vs
		WHERE tp.id = $1::uuid AND vt.code = 'identity' AND vs.code = 'verified'
		  AND NOT EXISTS (SELECT 1 FROM verifications v WHERE v.user_id = tp.user_id
		                  AND v.verification_type_id = vt.id AND v.verification_status_id = vs.id)`, profileID); err != nil {
		return fmt.Errorf("verification: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM availability_blocks WHERE talent_profile_id = $1::uuid AND note = 'demo seed'`, profileID); err != nil {
		return fmt.Errorf("clear availability: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO availability_blocks (talent_profile_id, starts_at, ends_at, is_available, note)
		VALUES ($1::uuid, now() - interval '30 days', now() + interval '400 days', true, 'demo seed')`, profileID); err != nil {
		return fmt.Errorf("availability: %w", err)
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
