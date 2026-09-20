package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	direction := flag.String("direction", "up", "up or down")
	steps := flag.Int("steps", 0, "number of steps (0 = all)")
	flag.Parse()

	cfg := config.Load("migrate")
	if err := run(cfg.DatabaseURL, *direction, *steps); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrate ok", "direction", *direction)
}

func run(databaseURL, direction string, steps int) error {
	m, err := migrate.New("file://db/migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	var migrateErr error
	switch direction {
	case "up":
		if steps > 0 {
			migrateErr = m.Steps(steps)
		} else {
			migrateErr = m.Up()
		}
	case "down":
		if steps > 0 {
			migrateErr = m.Steps(-steps)
		} else {
			migrateErr = m.Down()
		}
	default:
		return fmt.Errorf("unknown direction %q", direction)
	}
	if errors.Is(migrateErr, migrate.ErrNoChange) {
		return nil
	}
	return migrateErr
}
