// Command labd is the lab orchestrator and terminal gateway.
//
// Subcommands: serve (default) runs the session manager and the internal API; migrate
// applies the SQL migrations. The WebSocket gateway arrives in Phase 3.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/store"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const shutdownDrain = 5 * time.Second

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "labd:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout *os.File) error {
	fs := flag.NewFlagSet("labd", flag.ContinueOnError)
	cfgPath := fs.String("config", "labd.yaml", "path to labd.yaml")
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "usage: labd [-config labd.yaml] [serve|migrate]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintln(stdout, "labd", version)
		return nil
	}
	cmd := "serve"
	if fs.NArg() > 0 {
		cmd = fs.Arg(0)
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("unexpected arguments after %s: %v", cmd, fs.Args()[1:])
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch cmd {
	case "serve":
		return runServe(ctx, *cfgPath, cfg, log)
	case "migrate":
		return migrate(ctx, cfg, stdout)
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// migrate applies labd's embedded SQL migrations (ADR 0003, ADR 0011).
func migrate(ctx context.Context, cfg config.Config, stdout *os.File) error {
	db, err := store.OpenPostgres(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	applied, err := db.Migrate(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		fmt.Fprintln(stdout, "migrate: up to date")
	}
	for _, v := range applied {
		fmt.Fprintln(stdout, "migrate: applied", v)
	}
	return nil
}

// serveHTTP runs h on ln until ctx is cancelled, then drains.
func serveHTTP(ctx context.Context, ln net.Listener, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("labd listening", "addr", ln.Addr().String(), "version", version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down; labs keep running", "drain", shutdownDrain)
	sctx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
