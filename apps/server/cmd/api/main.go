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
	"github.com/KingYoSun/koujou-san/apps/server/internal/automation"
	"github.com/KingYoSun/koujou-san/apps/server/internal/contexts"
	"github.com/KingYoSun/koujou-san/apps/server/internal/githubobserve"
	"github.com/KingYoSun/koujou-san/apps/server/internal/graphindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/httpapi"
	"github.com/KingYoSun/koujou-san/apps/server/internal/inspections"
	"github.com/KingYoSun/koujou-san/apps/server/internal/mcpapi"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"github.com/KingYoSun/koujou-san/apps/server/internal/publications"
	"github.com/KingYoSun/koujou-san/apps/server/internal/runs"
	"github.com/KingYoSun/koujou-san/apps/server/internal/searchindex"
	"github.com/KingYoSun/koujou-san/apps/server/internal/sharedsync"
	"github.com/KingYoSun/koujou-san/apps/server/internal/usage"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workbench"
	"github.com/KingYoSun/koujou-san/apps/server/internal/workflows"
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
			githubConfig, err := githubobserve.Environment()
			if err != nil {
				slog.Error("GitHub configuration invalid")
				os.Exit(1)
			}
			observer, err := githubobserve.New(ctx, store, accessService, githubConfig)
			if err != nil {
				slog.Error("GitHub observation initialization failed")
				os.Exit(1)
			}
			observer.Start(ctx)
			routes = append(routes, observer.Register)
			limits, err := contexts.EnvironmentLimits()
			if err != nil {
				slog.Error("Context configuration invalid")
				os.Exit(1)
			}
			contextService := contexts.New(store, accessService, limits)
			searchService := searchindex.New(store, accessService, contextService, os.Getenv("KOUJOU_SEARCH_DIR"), service.Config.Key)
			searchService.Start(ctx, os.Getenv("KOUJOU_SEARCH_REBUILD") == "1")
			defer searchService.Close()
			routes = append(routes, searchService.Register)
			graphLimits, err := graphindex.EnvironmentLimits()
			if err != nil {
				slog.Error("graph configuration invalid")
				os.Exit(1)
			}
			var remote *graphindex.Remote
			if address := os.Getenv("KOUJOU_GRAPH_URL"); address != "" {
				remote, err = graphindex.NewRemote(address, os.Getenv("KOUJOU_GRAPH_USER"), os.Getenv("KOUJOU_GRAPH_PASSWORD"), os.Getenv("KOUJOU_GRAPH_DATABASE"))
				if err != nil {
					slog.Error("graph connection configuration invalid")
					os.Exit(1)
				}
			}
			graphService := graphindex.New(store, accessService, contextService, remote, graphLimits, service.Config.Key)
			graphService.Start(ctx)
			defer graphService.Close()
			routes = append(routes, graphService.Register)
			routes = append(routes, contextService.Register, sharedsync.New(store, accessService, contextService, service.Config.Key).Register)
			usageService := usage.New(store, accessService)
			routes = append(routes, usageService.Register)
			automationService := automation.New(store, accessService)
			routes = append(routes, automationService.Register)
			runService := runs.New(store, accessService, contextService)
			routes = append(routes, runService.Register)
			workflowService := workflows.New(store, accessService, contextService, runService)
			inspectionService := inspections.New(store, accessService, contextService, runService, workflowService)
			routes = append(routes, inspectionService.Register)
			publicationConfig, err := publications.Environment()
			if err != nil {
				slog.Error("issue publication configuration invalid")
				os.Exit(1)
			}
			publicationService := publications.New(store, accessService, runService, publications.NewRemote(publicationConfig))
			publicationService.StartWorker(ctx)
			routes = append(routes, publicationService.Register)
			routes = append(routes, workbench.New(store, accessService, runService, workflowService).Register)
			workflowService.Observer = observer
			workflowService.StartWorker(ctx)
			routes = append(routes, workflowService.Register)
			routes = append(routes, (&mcpapi.Service{Auth: service, Access: accessService, Contexts: contextService, Search: searchService, Graph: graphService, Usage: usageService, Automation: automationService, Runs: runService, Workflows: workflowService}).Register)
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
