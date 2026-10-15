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

	"github.com/KingYoSun/koujou-san/apps/server/internal/access"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/httpapi"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/sharedsync"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var ready func(context.Context) error
	var routes []func(*http.ServeMux)
	if databaseURL := os.Getenv("KOUJOU_DATABASE_URL"); databaseURL != "" {
		store, err := persistence.Open(ctx, databaseURL)
		if err != nil {
			slog.Error("API database initialization failed")
			os.Exit(1)
		}
		defer store.Close()
		ready = store.Ready
		if os.Getenv("KOUJOU_AUTH_ENABLED") == "1" {
			service, err := auth.New(ctx, auth.Environment(), store.Database())
			if err != nil {
				slog.Error("authentication initialization failed")
				os.Exit(1)
			}
			routes = append(routes, service.Register)
			accessService := access.New(store, service.Config.Key, func(w http.ResponseWriter, r *http.Request) (string, bool) {
				user, ok := service.RequestUser(w, r)
				return user.ID, ok
			})
			routes = append(routes, accessService.Register)
			limits, err := contexts.EnvironmentLimits()
			if err != nil {
				slog.Error("Context configuration invalid")
				os.Exit(1)
			}
			contextService := contexts.New(store, accessService, limits)
			routes = append(routes, contextService.Register, sharedsync.New(store, accessService, contextService, service.Config.Key).Register)
		}
	}
	address := os.Getenv("KOUJOU_API_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}

	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.NewHandler(ready, routes...),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverError := make(chan error, 1)
	go func() {
		slog.Info("starting development API", "address", address)
		if cert := os.Getenv("KOUJOU_TLS_CERT"); cert != "" {
			serverError <- server.ListenAndServeTLS(cert, os.Getenv("KOUJOU_TLS_KEY"))
		} else if os.Getenv("KOUJOU_AUTH_ENABLED") == "1" {
			serverError <- errors.New("authentication requires TLS")
		} else {
			serverError <- server.ListenAndServe()
		}
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
