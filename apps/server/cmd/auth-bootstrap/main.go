package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"os"
	"strings"
	"time"
)

func main() {
	email := flag.String("admin-email", "", "explicit initial administrator email")
	passwordFile := flag.String("password-file", "", "path to a private password file")
	flag.Parse()
	if *email == "" || *passwordFile == "" {
		fmt.Fprintln(os.Stderr, "--admin-email and --password-file are required")
		os.Exit(2)
	}
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		fmt.Fprintln(os.Stderr, "password file unavailable")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := persistence.Open(ctx, os.Getenv("KOUJOU_DATABASE_URL"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "database unavailable")
		os.Exit(1)
	}
	defer store.Close()
	service, err := auth.New(ctx, auth.Environment(), store.Database())
	if err == nil {
		err = service.Bootstrap(ctx, *email, strings.TrimSpace(string(password)))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "authentication bootstrap failed")
		os.Exit(1)
	}
	fmt.Println("OAuth clients and explicit administrator are configured; email verification is required.")
}
