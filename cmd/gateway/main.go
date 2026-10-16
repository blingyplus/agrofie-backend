package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/blingyplus/agrofie-backend/graph"
	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
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
	"github.com/blingyplus/agrofie-backend/internal/payments"
	"github.com/blingyplus/agrofie-backend/internal/review"
	"github.com/blingyplus/agrofie-backend/internal/talentprofile"
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
	fakePayments := cfg.PaystackSecretKey == "" || cfg.PaymentsProvider == "fake"
	if fakePayments {
		paymentsProvider = payments.FakeProvider{BaseURL: cfg.PublicBaseURL}
		slog.Warn("no PAYSTACK_SECRET_KEY (or PAYMENTS_PROVIDER=fake): using FAKE test payment provider (no real money). Checkout pages at /pay/fake/{reference}", "publicBaseURL", cfg.PublicBaseURL)
	} else {
		paymentsProvider = payments.NewPaystack(cfg.PaystackSecretKey)
	}
	talentProfiles := talentprofile.NewService(pool, paymentsProvider, cfg.CommissionPercent)
	verifications := verification.NewService(pool)
	availabilitySvc := availability.NewService(pool)
	bookings := booking.NewService(pool, availabilitySvc).AllowEarlyCompletion(cfg.DevAllowEarlyComplete)
	if cfg.DevAllowEarlyComplete {
		slog.Warn("DEV_ALLOW_EARLY_COMPLETE on: bookings may be completed before the event ends")
	}
	checkoutSvc := checkout.NewService(pool, paymentsProvider, bookings, cfg.CommissionPercent)
	reviewsSvc := review.NewService(pool)
	disputesSvc := dispute.NewService(pool)

	prober := probe.NewConnectProber(cfg.AuthURL, cfg.BookingURL, cfg.PaymentsURL)
	authClient := graph.NewAuthClient(cfg.AuthURL)
	resolver := &graph.Resolver{
		Lookups:        lookups,
		Discovery:      discoverySvc,
		TalentProfiles: talentProfiles,
		Verifications:  verifications,
		Availability:   availabilitySvc,
		Bookings:       bookings,
		Checkout:       checkoutSvc,
		Reviews:        reviewsSvc,
		Disputes:       disputesSvc,
		Prober:         prober,
		AuthClient:     authClient,
	}

	schemaCfg := graph.Config{Resolvers: resolver}
	schemaCfg.Directives.Authenticated = graph.Authenticated
	schemaCfg.Directives.HasRole = graph.HasRole
	gql := handler.NewDefaultServer(graph.NewExecutableSchema(schemaCfg))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))
	mux.Handle("/graphql", gqlauth.Middleware(authClient)(gql))
	mux.Handle("/playground", playground.Handler("Agrofie GraphQL", "/graphql"))
	mux.HandleFunc("POST /webhooks/paystack", paystackWebhookHandler(checkoutSvc))
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
		_, _ = fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><body style="font-family:system-ui;text-align:center;margin-top:60px"><h2>Test payment: %s</h2><p>Return to the Agrofie app and tap "I've paid" / refresh.</p>`, html.EscapeString(event))
	}
}
