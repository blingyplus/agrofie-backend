package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/health"
	"github.com/blingyplus/agrofie-backend/internal/payments"
)

func main() {
	cfg := config.Load("payments")
	_ = payments.FakeProvider{} // wire real Paystack adapter later

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Handler(cfg.ServiceName))

	slog.Info("payments service listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), mux); err != nil {
		slog.Error("payments stopped", "err", err)
		os.Exit(1)
	}
}
