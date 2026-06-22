package main

import (
	"log/slog"
	"net/http"

	"github.com/tomoropy/gcp-outbox-poc/internal/config"
	"github.com/tomoropy/gcp-outbox-poc/services/simulator"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	addr := ":" + cfg.Port
	slog.Info("simulator listening", slog.String("addr", addr))
	if err := http.ListenAndServe(addr, simulator.New().Handler()); err != nil {
		panic(err)
	}
}
