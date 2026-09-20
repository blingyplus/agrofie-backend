package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/blingyplus/agrofie-backend/internal/config"
	"github.com/blingyplus/agrofie-backend/internal/httpsvc"
)

func main() {
	cfg := config.Load("booking")
	handler := httpsvc.BookingHandler(cfg.ServiceName)
	slog.Info("booking service listening", "addr", cfg.Addr())
	if err := http.ListenAndServe(cfg.Addr(), handler); err != nil {
		slog.Error("booking stopped", "err", err)
		os.Exit(1)
	}
}
