package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/health"
)

func main() {
	cfg := config.Load("auth")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))

	slog.Info("auth service listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), mux); err != nil {
		slog.Error("auth stopped", "err", err)
		os.Exit(1)
	}
}
