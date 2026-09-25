package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/auth"
	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/httpsvc"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load("auth")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	kratos := auth.NewKratos(cfg.KratosAdminURL, cfg.KratosPublicURL)
	svc := auth.NewService(pool, kratos)
	handler := httpsvc.AuthHandler(cfg.ServiceName, svc)

	slog.Info("auth service listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), handler); err != nil {
		slog.Error("auth stopped", "err", err)
		os.Exit(1)
	}
}
