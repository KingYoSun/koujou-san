package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/auth"
	"github.com/KingYoSun/koujou-san/apps/server/internal/persistence"
	"io"
	"os"
	"strings"
	"time"
)

func main() {
	email := flag.String("admin-email", "", "explicit initial administrator email")
	passwordFile := flag.String("password-file", "", "path to a private password file")
	passwordStdin := flag.Bool("password-stdin", false, "read the initial password from standard input")
	clientsOnly := flag.Bool("clients-only", false, "register bundled OAuth clients without changing identities")
	flag.Parse()
	if !*clientsOnly && (*email == "" || (*passwordFile == "") == !*passwordStdin) {
		fmt.Fprintln(os.Stderr, "--admin-email and --password-file are required")
		os.Exit(2)
	}
	var password []byte
	var err error
	if !*clientsOnly {
		if *passwordStdin {
			password, err = io.ReadAll(os.Stdin)
		} else {
			password, err = os.ReadFile(*passwordFile)
		}
	}
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
		if *clientsOnly {
			err = service.ProvisionClients(ctx)
		} else {
			err = service.Bootstrap(ctx, *email, strings.TrimSpace(string(password)))
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "authentication bootstrap failed")
		os.Exit(1)
	}
	if *clientsOnly {
		fmt.Println("Bundled OAuth clients are configured.")
	} else {
		fmt.Println("OAuth clients and explicit administrator are configured; email verification is required.")
	}
}
