package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"gdblabs/labd/internal/api"
	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/metrics"
	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
	"gdblabs/labd/internal/term"
	"gdblabs/labd/sandbox"
	"gdblabs/labd/testpage"
)

// runServe is `labd serve`: migrate, load challenges, connect to containerd, reconcile,
// then serve the internal API until SIGINT or SIGTERM. Labs keep running across restarts.
func runServe(ctx context.Context, cfgPath string, cfg config.Config, log *slog.Logger) error {
	if cfg.InternalSecret == "" {
		return fmt.Errorf("%s is not set; the internal API refuses to run without it (S11)", config.EnvInternalSecret)
	}
	if cfg.WSTokenKey == "" {
		return fmt.Errorf("%s is not set; the terminal gateway refuses to run without it (S12)", config.EnvWSTokenKey)
	}
	// Registered first, so it runs last: after the recorder, the manager and the stores have
	// closed. Its absence after "shutting down" means labd hung.
	defer log.Info("labd stopped; labs keep running")
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
		WSGrace: cfg.WSReconnectGrace(),
	}, rt, db, clock.Real{}, log)
	defer m.Close()
	rep, err := m.Reconcile(ctx)
	if err != nil {
		// Some containers could not be removed; they are retried on the next restart.
		log.Error("reconcile incomplete", "err", err)
	}
	log.Info("reconcile done", "adopted", rep.Adopted, "removed", rep.Removed, "closed_rows", rep.ClosedRows)

	rl := &reloader{path: cfgPath, current: cfg, m: m, rt: rt, log: log}
	srv, err := api.New(m, cfg.InternalSecret, rl.Reload, log)
	if err != nil {
		return err
	}
	srv.PendingPull = pendingPull(m, rt)
	tokens := term.NewTokens([]byte(cfg.WSTokenKey))
	if cfg.DevMintTokens {
		log.Warn("dev_mint_tokens is on: POST /internal/sessions returns a ws_token (never in production, ADR 0015)")
		srv.MintToken = func(id string, uid int64) string { return tokens.Mint(id, uid, time.Now()) }
	}
	rec := term.NewRecorder(db, clock.Real{}, log)
	defer rec.Close()
	gw := term.New(term.Options{
		Sessions: m, Tokens: tokens, Recorder: rec, Log: log,
		SiteOrigin: strings.TrimSuffix(cfg.SiteHost, "/"), AllowNoOrigin: cfg.DevAllowNoOrigin,
	})
	// Resource samples every metrics_flush_s (Phase 7). Stopped before the store closes.
	sampler := &metrics.Sampler{
		Root: "/", CgroupDir: "sys/fs/cgroup" + orch.CgroupParent, DiskPath: containerdRoot,
		Src: m, WSBytes: gw.Bytes, Sink: db, OnRSS: m.ObserveRSS, Log: log,
	}
	sctx, stopSampler := context.WithCancel(ctx)
	var sampling sync.WaitGroup
	sampling.Add(1)
	go func() { defer sampling.Done(); sampler.Run(sctx, cfg.MetricsFlush()) }()
	defer func() { stopSampler(); sampling.Wait() }()

	wsMux := http.NewServeMux()
	wsMux.Handle("/ws/", gw.Handler())
	if cfg.DevTestpage {
		log.Warn("dev_testpage is on: /dev/term serves the xterm.js test page (never in production)")
		wsMux.Handle("GET /dev/term", testpage.Handler())
		wsMux.Handle("GET /dev/vendor/", testpage.Handler())
	}

	lnAPI, err := api.Listen(cfg.ListenInternal)
	if err != nil {
		return err
	}
	lnWS, err := api.Listen(cfg.ListenWS)
	if err != nil {
		lnAPI.Close()
		return err
	}

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	go func() {
		sweep := time.NewTicker(time.Minute)
		defer sweep.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-sweep.C:
				tokens.Sweep(now)
			case <-hup:
				if sum, err := rl.Reload(ctx); err != nil {
					log.Error("SIGHUP reload failed; keeping the previous config", "err", err)
				} else {
					log.Info("SIGHUP reload", "summary", sum)
				}
			}
		}
	}()

	return serveAll(ctx, log, gw.Shutdown,
		listener{"internal API", lnAPI, srv.Handler()},
		listener{"terminal gateway", lnWS, wsMux})
}

// pendingPull counts enabled challenges whose image is not in containerd, for the admin
// page (ADR 0018). The answer is kept 30 s: the dashboards poll stats.
func pendingPull(m *orch.Manager, rt *orch.ContainerdRuntime) func(context.Context) int {
	var (
		mu   sync.Mutex
		at   time.Time
		last int
	)
	return func(ctx context.Context) int {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(at) < 30*time.Second {
			return last
		}
		last, at = len(rt.Missing(ctx, orch.ChallengeImages(m.Challenges()))), time.Now()
		return last
	}
}

// containerdRoot is containerd's default root (provision.sh keeps it): host.disk_used_gb is
// the filesystem holding images and snapshots.
const containerdRoot = "/var/lib/containerd"

type listener struct {
	name string
	ln   net.Listener
	h    http.Handler
}

// serveAll serves every listener until ctx is cancelled or one fails (which stops the
// others), then closes the gateway's WebSockets with closeWS. http.Server.Shutdown does not
// track hijacked connections, so without this they would only drop when the process exits,
// with no close frame. Labs keep running either way.
func serveAll(ctx context.Context, log *slog.Logger, closeWS func(context.Context) error, ls ...listener) error {
	sctx, stop := context.WithCancel(ctx)
	defer stop()
	errc := make(chan error, len(ls))
	for _, l := range ls {
		go func() { errc <- serveHTTP(sctx, l.name, l.ln, l.h, log) }()
	}
	err := <-errc
	if ctx.Err() != nil {
		log.Info("shutting down; labs keep running")
	}
	stop()
	for range ls[1:] {
		if err2 := <-errc; err == nil {
			err = err2
		}
	}
	dctx, cancel := context.WithTimeout(context.Background(), shutdownDrain)
	defer cancel()
	if err2 := closeWS(dctx); err2 != nil {
		log.Warn("gateway shutdown incomplete", "err", err2)
	}
	return err
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
	rt      *orch.ContainerdRuntime
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
	r.m.SetWSGrace(cfg.WSReconnectGrace())
	r.m.SetCap(cfg.MaxSessions)
	r.current = cfg
	// Pre-pull images new to challenges.json (spec), in the background: a pull can take
	// minutes and must not hold the reload request.
	go func(images []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if res, err := r.rt.Pull(ctx, images); err != nil {
			r.log.Error("reload: pre-pull failed", "err", err)
		} else {
			r.log.Info("reload: images ready", "images", len(res))
		}
	}(orch.ChallengeImages(cs))
	return map[string]any{"max_sessions": cfg.MaxSessions, "max_queue": cfg.MaxQueue, "challenges": len(cs)}, nil
}
