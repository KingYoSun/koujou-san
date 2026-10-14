package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/KingYoSun/koujou-san/apps/server/internal/httpapi"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var ready func(context.Context) error
	if databaseURL := os.Getenv("KOUJOU_DATABASE_URL"); databaseURL != "" {
		store, err := persistence.Open(ctx, databaseURL)
		if err != nil {
			slog.Error("API database initialization failed")
			os.Exit(1)
		}
		defer store.Close()
		ready = store.Ready
	}
	address := os.Getenv("KOUJOU_API_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}

	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.NewHandler(ready),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)
	go func() {
		slog.Info("starting development API", "address", address)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("API stopped", "error", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			slog.Error("API shutdown failed", "error", err)
			os.Exit(1)
		}
	}
}
