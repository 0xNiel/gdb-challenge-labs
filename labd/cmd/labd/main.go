// Command labd is the lab orchestrator and terminal gateway.
//
// Subcommands: serve (default) runs the session manager and the internal API; migrate
// applies the SQL migrations; pull and prune manage challenge images. serve also runs the
// WebSocket terminal gateway.
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
	"gdblabs/labd/internal/orch"
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
		fmt.Fprintln(fs.Output(), "usage: labd [-config labd.yaml] [serve|migrate|pull|prune]")
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
	case "pull":
		return pull(ctx, cfg, stdout)
	case "prune":
		return prune(ctx, cfg, stdout)
	default:
		fs.Usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

// pull makes every enabled challenge image present and labels it lab.keep=true.
func pull(ctx context.Context, cfg config.Config, stdout *os.File) error {
	cs, err := orch.LoadChallenges(cfg.ChallengesFile)
	if err != nil {
		return err
	}
	rt, err := orch.NewContainerdRuntime(cfg.ContainerdSocket, cfg.Runtime, "")
	if err != nil {
		return err
	}
	defer rt.Close()
	res, err := rt.Pull(ctx, orch.ChallengeImages(cs))
	for _, r := range res {
		switch {
		case r.Err != "":
			fmt.Fprintf(stdout, "pull: FAILED  %s: %s\n", r.Image, r.Err)
		case r.Present:
			fmt.Fprintf(stdout, "pull: present %s\n", r.Image)
		default:
			fmt.Fprintf(stdout, "pull: pulled  %s\n", r.Image)
		}
	}
	if err != nil {
		return fmt.Errorf("pull failed for %d of %d images (see above)", countFailed(res), len(res))
	}
	return nil
}

func countFailed(rs []orch.PullResult) int {
	n := 0
	for _, r := range rs {
		if r.Err != "" {
			n++
		}
	}
	return n
}

// prune lists images no enabled challenge uses. Deletion and the 14-day rule are Phase 5.
func prune(ctx context.Context, cfg config.Config, stdout *os.File) error {
	cs, err := orch.LoadChallenges(cfg.ChallengesFile)
	if err != nil {
		return err
	}
	rt, err := orch.NewContainerdRuntime(cfg.ContainerdSocket, cfg.Runtime, "")
	if err != nil {
		return err
	}
	defer rt.Close()
	names, err := rt.Unreferenced(ctx, orch.ChallengeImages(cs))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "prune: %d images not used by an enabled challenge (listing only; deletion comes in Phase 5)\n", len(names))
	for _, n := range names {
		fmt.Fprintln(stdout, "  ", n)
	}
	return nil
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

// serveHTTP runs h on ln until ctx is cancelled, then drains. name labels the log lines.
// Shutdown does not wait for hijacked connections (WebSockets); the gateway closes those
// itself (serveAll).
func serveHTTP(ctx context.Context, name string, ln net.Listener, h http.Handler, log *slog.Logger) error {
	srv := &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	log.Info("labd listening", "listener", name, "addr", ln.Addr().String(), "version", version)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("listener shutting down", "listener", name, "addr", ln.Addr().String(), "drain", shutdownDrain)
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
