package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/99designs/gqlgen/graphql/playground"
	"github.com/blingyplus/agrofie-backend/graph"
	"github.com/blingyplus/agrofie-backend/graph/gqlauth"
	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/db"
	"github.com/blingyplus/agrofie-backend/internal/gateway/probe"
	"github.com/blingyplus/agrofie-backend/internal/health"
	"github.com/blingyplus/agrofie-backend/internal/lookup"
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
	prober := probe.NewConnectProber(cfg.AuthURL, cfg.BookingURL, cfg.PaymentsURL)
	authClient := graph.NewAuthClient(cfg.AuthURL)
	resolver := &graph.Resolver{
		Lookups:    lookups,
		Prober:     prober,
		AuthClient: authClient,
	}

	schemaCfg := graph.Config{Resolvers: resolver}
	schemaCfg.Directives.Authenticated = graph.Authenticated
	schemaCfg.Directives.HasRole = graph.HasRole
	gql := handler.NewDefaultServer(graph.NewExecutableSchema(schemaCfg))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))
	mux.Handle("/graphql", gqlauth.Middleware(authClient)(gql))
	mux.Handle("/playground", playground.Handler("Agrofie GraphQL", "/graphql"))

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
