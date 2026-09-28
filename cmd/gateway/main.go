package main

import (
	"context"
	"errors"
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

	var paymentsProvider payments.Provider = payments.FakeProvider{}
	if cfg.PaystackSecretKey != "" {
		paymentsProvider = payments.NewPaystack(cfg.PaystackSecretKey)
	} else {
		slog.Warn("PAYSTACK_SECRET_KEY not set; using FakeProvider for payouts")
	}
	talentProfiles := talentprofile.NewService(pool, paymentsProvider, cfg.CommissionPercent)
	verifications := verification.NewService(pool)
	availabilitySvc := availability.NewService(pool)
	bookings := booking.NewService(pool, availabilitySvc)
	checkoutSvc := checkout.NewService(pool, paymentsProvider, bookings, cfg.CommissionPercent)
	reviewsSvc := review.NewService(pool)

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
