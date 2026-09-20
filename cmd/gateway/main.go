package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/gateway"
	"github.com/blingyplus/agrofie-backend/internal/health"
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

	resolver := gateway.NewResolver(pool, cfg.AuthURL, cfg.BookingURL, cfg.PaymentsURL)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))
	mux.Handle("/graphql", &gateway.GraphQLHandler{Resolver: resolver})
	mux.Handle("/playground", &gateway.GraphQLHandler{Resolver: resolver})

	slog.Info("gateway listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), withCORS(mux)); err != nil {
		slog.Error("gateway stopped", "err", err)
		os.Exit(1)
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
