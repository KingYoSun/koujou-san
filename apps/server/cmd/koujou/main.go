package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/KingYoSun/koujou-san/apps/server/internal/distribution"
	"os"
	"os/signal"
	"runtime"
	"syscall"
)

func main() {
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		fmt.Fprintln(os.Stderr, "Linux x86_64 required")
		os.Exit(2)
	}
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "koujou init|up|stop|status|bootstrap|backup|restore|update --state DIR (see OPERATIONS.md)")
		os.Exit(2)
	}
	command := os.Args[1]
	f := flag.NewFlagSet(command, flag.ExitOnError)
	state := f.String("state", "", "private deployment directory")
	bundle := f.String("bundle", "", "unpacked server bundle directory")
	site := f.String("url", "", "public HTTPS origin")
	smtp := f.String("smtp-file", "", "private file containing SMTP URI")
	cert := f.String("cert", "", "TLS full chain PEM")
	key := f.String("key", "", "TLS private key PEM")
	ca := f.String("ca", "/etc/ssl/certs/ca-certificates.crt", "trusted CA bundle PEM")
	email := f.String("admin-email", "", "initial administrator email")
	password := f.String("password-file", "", "private initial password file")
	backup := f.String("backup", "", "backup input/output path")
	llm := f.Bool("llm", false, "enable the optional bundled Gateway")
	f.Parse(os.Args[2:])
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root, err := distribution.Absolute(*state)
	var release string
	if err == nil && *bundle != "" {
		release, err = distribution.Absolute(*bundle)
	}
	if err == nil {
		switch command {
		case "init":
			err = distribution.Init(release, root, *site, *smtp, *cert, *key, *ca)
		case "restore":
			err = distribution.Restore(ctx, release, root, *backup)
		default:
			err = func() error {
				d, e := distribution.Open(root)
				if e != nil {
					return e
				}
				if command == "status" {
					if e = distribution.PrintStatus(os.Stdout, d.Settings); e != nil {
						return e
					}
					return d.Status(ctx, os.Stdout)
				}
				unlock, e := d.Lock()
				if e != nil {
					return e
				}
				defer unlock()
				switch command {
				case "up":
					if *llm {
						if e = d.EnableGateway(); e != nil {
							return e
						}
					}
					return d.Up(ctx)
				case "stop":
					return d.Stop(ctx)
				case "bootstrap":
					return d.Bootstrap(ctx, *email, *password)
				case "backup":
					return d.Backup(ctx, *backup)
				case "update":
					return d.Update(ctx, release)
				default:
					return fmt.Errorf("unknown operation")
				}
			}()
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if command != "status" {
		fmt.Println("Operation completed.")
	}
}
