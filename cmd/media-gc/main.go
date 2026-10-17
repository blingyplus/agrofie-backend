// media-gc removes uploads nothing references (never attached to a talent
// profile or verification) once they are older than -older-than.
// Usage: go run ./cmd/media-gc [-older-than 24h] [-dry-run]
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/media"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	olderThan := flag.Duration("older-than", 24*time.Hour, "only collect media older than this")
	dry := flag.Bool("dry-run", false, "count only; delete nothing")
	flag.Parse()
	cfg := config.Load("media-gc")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	store, err := media.NewLocalDisk(cfg.MediaDir)
	if err != nil {
		slog.Error("media dir", "err", err)
		os.Exit(1)
	}
	svc := media.NewService(pool, store, cfg.PublicBaseURL, nil)
	n, err := svc.CollectOrphans(ctx, time.Now().Add(-*olderThan), *dry)
	if err != nil {
		slog.Error("media gc failed", "err", err)
		os.Exit(1)
	}
	verb := "removed"
	if *dry {
		verb = "would remove"
	}
	fmt.Printf("media-gc: %s %d orphaned upload(s)\n", verb, n)
}
