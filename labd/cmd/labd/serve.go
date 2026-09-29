package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"gdblabs/labd/internal/api"
	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
	"gdblabs/labd/sandbox"
)

// runServe is `labd serve`: migrate, load challenges, connect to containerd, reconcile,
// then serve the internal API until SIGINT or SIGTERM. Labs keep running across restarts.
func runServe(ctx context.Context, cfgPath string, cfg config.Config, log *slog.Logger) error {
	if cfg.InternalSecret == "" {
		return fmt.Errorf("%s is not set; the internal API refuses to run without it (S11)", config.EnvInternalSecret)
	}
	db, err := store.OpenPostgres(ctx, cfg.PostgresDSN)
	if err != nil {
		return err
	}
	defer db.Close()
	if applied, err := db.Migrate(ctx); err != nil {
		return err
	} else if len(applied) > 0 {
		log.Info("migrations applied", "versions", applied)
	}
	challenges, err := orch.LoadChallenges(cfg.ChallengesFile)
	if err != nil {
		return err
	}
	rt, err := orch.NewContainerdRuntime(cfg.ContainerdSocket, cfg.Runtime, fifoDir())
	if err != nil {
		return err
	}
	defer rt.Close()

	m := orch.NewManager(orch.ManagerConfig{
		BaseSpec: sandbox.Base, Runtime: cfg.Runtime, MaxSessions: cfg.MaxSessions,
		MaxQueue: cfg.MaxQueue, DefaultLimits: cfg.DefaultLimits, Challenges: challenges,
	}, rt, db, clock.Real{}, log)
	defer m.Close()
	rep, err := m.Reconcile(ctx)
	if err != nil {
		// Some containers could not be removed; they are retried on the next restart.
		log.Error("reconcile incomplete", "err", err)
	}
	log.Info("reconcile done", "adopted", rep.Adopted, "removed", rep.Removed, "closed_rows", rep.ClosedRows)

	rl := &reloader{path: cfgPath, current: cfg, m: m, log: log}
	srv, err := api.New(m, cfg.InternalSecret, rl.Reload, log)
	if err != nil {
		return err
	}
	ln, err := api.Listen(cfg.ListenInternal)
	if err != nil {
		return err
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-hup:
				if sum, err := rl.Reload(ctx); err != nil {
					log.Error("SIGHUP reload failed; keeping the previous config", "err", err)
				} else {
					log.Info("SIGHUP reload", "summary", sum)
				}
			}
		}
	}()
	return serveHTTP(ctx, ln, srv.Handler(), log)
}

// fifoDir is where labd keeps the terminal FIFOs: systemd's RuntimeDirectory in production,
// a per-user directory otherwise. It must survive a labd restart (adoption re-opens them).
func fifoDir() string {
	if d := os.Getenv("RUNTIME_DIRECTORY"); d != "" {
		return filepath.Join(d, "fifo")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("labd-%d", os.Getuid()), "fifo")
}

// reloader applies labd.yaml and challenges.json changes that do not need a restart.
type reloader struct {
	path    string
	log     *slog.Logger
	m       *orch.Manager
	mu      sync.Mutex
	current config.Config
}

func (r *reloader) Reload(context.Context) (map[string]any, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg, err := config.Load(r.path)
	if err != nil {
		return nil, err
	}
	cs, err := orch.LoadChallenges(cfg.ChallengesFile)
	if err != nil {
		return nil, err
	}
	old := r.current
	for name, changed := range map[string]bool{
		"listen_internal": cfg.ListenInternal != old.ListenInternal, "listen_ws": cfg.ListenWS != old.ListenWS,
		"containerd_socket": cfg.ContainerdSocket != old.ContainerdSocket, "runtime": cfg.Runtime != old.Runtime,
		"postgres_dsn": cfg.PostgresDSN != old.PostgresDSN, "internal secret": cfg.InternalSecret != old.InternalSecret,
	} {
		if changed {
			r.log.Warn("reload: this setting changes only on restart", "setting", name)
		}
	}
	r.m.SetDefaultLimits(cfg.DefaultLimits)
	r.m.SetChallenges(cs)
	r.m.SetMaxQueue(cfg.MaxQueue)
	r.m.SetCap(cfg.MaxSessions)
	r.current = cfg
	return map[string]any{"max_sessions": cfg.MaxSessions, "max_queue": cfg.MaxQueue, "challenges": len(cs)}, nil
}
