package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/auth"
	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	cfg := config.Load("seed-admin")
	if cfg.AdminPassword == "" {
		slog.Error("ADMIN_PASSWORD is required")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	svc := auth.NewService(pool, auth.NewKratos(cfg.KratosAdminURL, cfg.KratosPublicURL))
	user, err := svc.SeedAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword, cfg.AdminDisplay)
	if err != nil {
		slog.Error("seed admin failed", "err", err)
		os.Exit(1)
	}
	fmt.Printf("admin ready: id=%s email=%s roles=%v\n", user.ID, user.Email, user.RoleCodes)
}
