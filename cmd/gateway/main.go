package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/blingyplus/agrofie-backend/graph"
	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
	"github.com/blingyplus/agrofie-backend/internal/appconfig"
	"github.com/blingyplus/agrofie-backend/internal/availability"
	"github.com/blingyplus/agrofie-backend/internal/booking"
	"github.com/blingyplus/agrofie-backend/internal/checkout"
	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/discovery"
	"github.com/blingyplus/agrofie-backend/internal/dispute"
	"github.com/blingyplus/agrofie-backend/internal/gateway/probe"
	"github.com/blingyplus/agrofie-backend/internal/health"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
	"github.com/blingyplus/agrofie-backend/internal/media"
	"github.com/blingyplus/agrofie-backend/internal/organizerprofile"
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/blingyplus/agrofie-backend/internal/review"
	"github.com/blingyplus/agrofie-backend/internal/talentprofile"
	"github.com/blingyplus/agrofie-backend/internal/talentrequest"
	"github.com/blingyplus/agrofie-backend/internal/verification"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load("gateway")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	lookups := lookup.NewService(db.New(pool))
	discoverySvc := discovery.NewService(db.New(pool))

	var paymentsProvider payments.Provider
	fakePayments := cfg.UsesFakePayments()
	if fakePayments {
		paymentsProvider = payments.FakeProvider{BaseURL: cfg.PublicBaseURL}
		slog.Warn("no PAYSTACK_SECRET_KEY (or PAYMENTS_PROVIDER=fake): using FAKE test payment provider (no real money). Checkout pages at /pay/fake/{reference}", "publicBaseURL", cfg.PublicBaseURL)
	} else {
		// Test mode only until launch review: refuse anything but sk_test_ keys.
		if !strings.HasPrefix(cfg.PaystackSecretKey, "sk_test_") {
			slog.Error("PAYSTACK_SECRET_KEY must be a Paystack TEST key (sk_test_...); live keys are not allowed yet")
			os.Exit(1)
		}
		paymentsProvider = payments.NewPaystack(cfg.PaystackSecretKey)
		slog.Warn("using REAL Paystack TEST-mode payment provider (test cards only, no real money). Webhook: POST /webhooks/paystack")
	}
	slog.Info("payments provider", "provider", cfg.PaymentsProviderName())
	talentProfiles := talentprofile.NewService(pool, paymentsProvider, cfg.CommissionPercent)
	verifications := verification.NewService(pool)
	availabilitySvc := availability.NewService(pool)
	bookings := booking.NewService(pool, availabilitySvc).AllowEarlyCompletion(cfg.DevAllowEarlyComplete)
	if cfg.DevAllowEarlyComplete {
		slog.Warn("DEV_ALLOW_EARLY_COMPLETE on: bookings may be completed before the event ends")
	}
	checkoutSvc := checkout.NewService(pool, paymentsProvider, bookings, cfg.CommissionPercent).WithCallbackURL(strings.TrimRight(cfg.PublicBaseURL, "/") + "/pay/return")
	reviewsSvc := review.NewService(pool)
	disputesSvc := dispute.NewService(pool)

	mediaStore, err := media.NewLocalDisk(cfg.MediaDir)
	if err != nil {
		slog.Error("media dir unavailable", "dir", cfg.MediaDir, "err", err)
		os.Exit(1)
	}
	mediaSecret := []byte(cfg.MediaSigningSecret)
	if len(mediaSecret) == 0 {
		mediaSecret = make([]byte, 32)
		if _, err := rand.Read(mediaSecret); err != nil {
			slog.Error("media secret", "err", err)
			os.Exit(1)
		}
		slog.Warn("MEDIA_SIGNING_SECRET unset: private media links use a per-process secret and expire on restart")
	}
	mediaSvc := media.NewService(pool, mediaStore, cfg.PublicBaseURL, mediaSecret)
	graph.PublicMediaURL = mediaSvc.URL

	prober := probe.NewConnectProber(cfg.AuthURL, cfg.BookingURL, cfg.PaymentsURL)
	authClient := graph.NewAuthClient(cfg.AuthURL)
	resolver := &graph.Resolver{
		Lookups:           lookups,
		Discovery:         discoverySvc,
		TalentProfiles:    talentProfiles,
		Verifications:     verifications,
		Availability:      availabilitySvc,
		Bookings:          bookings,
		Checkout:          checkoutSvc,
		Reviews:           reviewsSvc,
		Disputes:          disputesSvc,
		TalentRequests:    talentrequest.NewService(pool),
		OrganizerProfiles: organizerprofile.NewService(pool),
		Media:             mediaSvc,
		Prober:            prober,
		PaymentsProvider:  cfg.PaymentsProviderName(),
		AuthClient:        authClient,
	}

	schemaCfg := graph.Config{Resolvers: resolver}
	schemaCfg.Directives.Authenticated = graph.Authenticated
	schemaCfg.Directives.HasRole = graph.HasRole
	gql := handler.NewDefaultServer(graph.NewExecutableSchema(schemaCfg))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))
	mux.HandleFunc("GET /config", appconfig.Handler(appconfig.Load()))
	mux.Handle("/graphql", gqlauth.Middleware(authClient)(gql))
	mux.Handle("/playground", playground.Handler("Agrofie GraphQL", "/graphql"))
	mediaHandler := gqlauth.Middleware(authClient)(media.Handler(mediaSvc))
	mux.Handle("/media", mediaHandler)
	mux.Handle("/media/", mediaHandler)
	mux.HandleFunc("POST /webhooks/paystack", paystackWebhookHandler(checkoutSvc))
	mux.HandleFunc("GET /pay/return", payReturnPage)
	if fakePayments {
		mux.HandleFunc("GET /pay/fake/{reference}", fakePayPage)
		mux.HandleFunc("POST /pay/fake/{reference}", fakePayComplete(checkoutSvc))
	}

	slog.Info("gateway listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), withCORS(mux)); err != nil {
		slog.Error("gateway stopped", "err", err)
		os.Exit(1)
	}
}

// paystackWebhookHandler reads the raw request body (never the parsed
// GraphQL/JSON path) so the signature can be verified against the exact
// bytes Paystack signed, before anything in the body is trusted.
func paystackWebhookHandler(svc *checkout.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MiB cap
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		sig := r.Header.Get("x-paystack-signature")
		if err := svc.HandleWebhookEvent(r.Context(), body, sig); err != nil {
			if errors.Is(err, checkout.ErrInvalidInput) {
				slog.Warn("paystack webhook rejected", "err", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			slog.Error("paystack webhook handling failed", "err", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

const fakePayHTML = `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Agrofie TEST payment</title>
<style>body{font-family:system-ui,sans-serif;max-width:420px;margin:40px auto;padding:0 16px;text-align:center}
.tag{background:#fde68a;border-radius:8px;padding:8px;font-weight:600}button{font-size:18px;padding:14px 28px;margin:8px;border-radius:10px;border:0;background:#16a34a;color:#fff}
button.f{background:#dc2626}</style></head><body>
<p class="tag">TEST MODE - fake payment provider. No real money moves.</p>
<h2>Pay for booking</h2><p>Reference<br><code>%s</code></p>
<form method="POST"><button name="outcome" value="success">Pay (simulate success)</button>
<button class="f" name="outcome" value="failed">Simulate failure</button></form></body></html>`

// payReturnPage is the Paystack callback_url target. It carries no authority:
// payment status comes only from the signed webhook or verifyPayment. It
// hands the payer back to the app via the agrofie:// deep link (auto-try plus
// a big button, since browsers may block a scripted custom-scheme jump).
func payReturnPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref := q.Get("reference")
	if ref == "" {
		ref = q.Get("trxref")
	}
	v := url.Values{}
	if safeToken.MatchString(ref) {
		v.Set("reference", ref)
	}
	if b := q.Get("booking"); safeToken.MatchString(b) {
		v.Set("booking", b)
	}
	v.Set("status", "return")
	link := "agrofie://pay/return?" + v.Encode()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = fmt.Fprintf(w, payReturnHTML, html.EscapeString(link), template.JSEscapeString(link))
}

var safeToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

const payReturnHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Back to Agrofie</title>
<style>
:root{--bg:#fbf8f1;--ink:#2a1a20;--ink2:#6b5a60;--brand:#7a1f3d;--tint:#f6e3e8}
@media(prefers-color-scheme:dark){:root{--bg:#1a1114;--ink:#f3ecee;--ink2:#b9aab0;--brand:#b8325a;--tint:#3a1c27}}
body{margin:0;min-height:100vh;display:flex;align-items:center;justify-content:center;background:var(--bg);color:var(--ink);font-family:system-ui,-apple-system,sans-serif;text-align:center}
main{max-width:360px;padding:32px 24px}
.dot{width:72px;height:72px;border-radius:50%%;background:var(--tint);color:var(--brand);display:flex;align-items:center;justify-content:center;font-size:36px;margin:0 auto 20px}
h1{font-size:24px;margin:0 0 8px}p{color:var(--ink2);font-size:17px;line-height:1.45;margin:0 0 28px}
a.btn{display:block;background:var(--brand);color:#fff;padding:18px 24px;border-radius:16px;text-decoration:none;font-size:18px;font-weight:700}
small{display:block;margin-top:18px;color:var(--ink2)}
</style></head><body><main><div class="dot">&#10003;</div><h1>Back to Agrofie</h1>
<p>Tap the button to return to the app. It will confirm your payment and show your booking.</p>
<a class="btn" href="%s">Return to Agrofie</a><small>If nothing happens, switch back to the Agrofie app.</small>
<script>setTimeout(function(){location.replace('%s')},300)</script></main></body></html>`

func fakePayPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, fakePayHTML, html.EscapeString(r.PathValue("reference")))
}

// fakePayComplete simulates Paystack calling our webhook: it builds a
// charge.success/charge.failed event, signs it with the fake key and runs it
// through the same HandleWebhookEvent path (signature check, idempotent event
// insert, MarkPaid). Only registered when no PAYSTACK_SECRET_KEY is set.
func fakePayComplete(svc *checkout.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ref := r.PathValue("reference")
		event := "charge.success"
		if r.FormValue("outcome") == "failed" {
			event = "charge.failed"
		}
		body, _ := json.Marshal(map[string]any{
			"event": event, "data": map[string]any{"reference": ref, "status": event[7:], "fake": true},
		})
		if err := svc.HandleWebhookEvent(r.Context(), body, payments.FakeSignature(body)); err != nil {
			slog.Error("fake payment failed", "err", err)
			http.Error(w, "payment could not be applied: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><body style="font-family:system-ui;text-align:center;margin-top:60px"><h2>Test payment: %s</h2><p>Return to the Agrofie app and tap "I've paid" / refresh.</p><p><a href="agrofie://" style="display:inline-block;background:#7a1f3d;color:#fff;padding:16px 28px;border-radius:14px;text-decoration:none;font-size:18px;font-weight:700">Back to Agrofie</a></p>`, html.EscapeString(event))
	}
}
