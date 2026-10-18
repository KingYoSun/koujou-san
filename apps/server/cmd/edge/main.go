package main

import (
	"context"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/distribution"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if distribution.ServeEdge(ctx) != nil {
		fmt.Fprintln(os.Stderr, "edge unavailable")
		os.Exit(1)
	}
}
