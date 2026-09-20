package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/httpsvc"
)

func main() {
	cfg := config.Load("auth")
	handler := httpsvc.AuthHandler(cfg.ServiceName)
	slog.Info("auth service listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), handler); err != nil {
		slog.Error("auth stopped", "err", err)
		os.Exit(1)
	}
}
