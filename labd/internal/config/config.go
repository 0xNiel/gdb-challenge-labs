// Package config loads and validates labd.yaml (spec section "Orchestrator → Config").
//
// Only secrets and the Postgres DSN may be overridden from the environment; everything
// else belongs in the file (docs/CONVENTIONS.md).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Limits are per-session resource and time limits. A challenge manifest may override
// any of them; zero fields fall back to Config.DefaultLimits.
type Limits struct {
	MemoryMB      int `yaml:"memory_mb" json:"memory_mb"`
	CPUMillicores int `yaml:"cpu_millicores" json:"cpu_millicores"`
	Pids          int `yaml:"pids" json:"pids"`
	TTLMinutes    int `yaml:"ttl_minutes" json:"ttl_minutes"`
	IdleMinutes   int `yaml:"idle_minutes" json:"idle_minutes"`
	ExtendMinutes int `yaml:"extend_minutes" json:"extend_minutes"`
}

// Config mirrors labd.yaml.
type Config struct {
	ListenInternal    string `yaml:"listen_internal"`
	ListenWS          string `yaml:"listen_ws"`
	ContainerdSocket  string `yaml:"containerd_socket"`
	Runtime           string `yaml:"runtime"`
	MaxSessions       int    `yaml:"max_sessions"`
	MaxQueue          int    `yaml:"max_queue"`
	WSReconnectGraceS int    `yaml:"ws_reconnect_grace_s"`
	DefaultLimits     Limits `yaml:"default_limits"`
	ChallengesFile    string `yaml:"challenges_file"`
	PostgresDSN       string `yaml:"postgres_dsn"`
	MetricsFlushS     int    `yaml:"metrics_flush_s"`

	// SiteHost is the site's origin (scheme and host, e.g. https://labs.example.com): the only
	// Origin the WebSocket gateway accepts (S12).
	SiteHost string `yaml:"site_host"`
	// DevAllowNoOrigin accepts WebSocket requests without an Origin header (Go client, perf
	// scripts). DevTestpage serves the xterm.js test page at /dev/term. Both dev only.
	DevAllowNoOrigin bool `yaml:"dev_allow_no_origin"`
	DevTestpage      bool `yaml:"dev_testpage"`
	// DevMintTokens puts a ws_token in POST /internal/sessions responses, for labd-perf, the
	// replay client and the test page. web mints for browsers (ADR 0015). Dev only.
	DevMintTokens bool `yaml:"dev_mint_tokens"`

	// InternalSecret is the bearer token web uses on the internal API (S11).
	// Environment only: LABD_INTERNAL_SECRET. Never read from the file.
	InternalSecret string `yaml:"-"`
	// WSTokenKey is the HMAC key for WebSocket tokens, shared with web (S12).
	// Environment only: WS_TOKEN_KEY.
	WSTokenKey string `yaml:"-"`
}

// Environment variable names.
const (
	EnvPostgresDSN    = "LABD_POSTGRES_DSN"
	EnvInternalSecret = "LABD_INTERNAL_SECRET"
	EnvWSTokenKey     = "WS_TOKEN_KEY"
)

// Default returns the spec's defaults.
func Default() Config {
	return Config{
		ListenInternal:    "127.0.0.1:8081",
		ListenWS:          "127.0.0.1:8082",
		ContainerdSocket:  "/run/containerd/containerd.sock",
		Runtime:           "io.containerd.runsc.v1",
		MaxSessions:       100,
		MaxQueue:          50,
		WSReconnectGraceS: 60,
		DefaultLimits: Limits{
			MemoryMB:      128,
			CPUMillicores: 500,
			Pids:          32,
			TTLMinutes:    60,
			IdleMinutes:   15,
			ExtendMinutes: 15,
		},
		ChallengesFile: "/etc/labd/challenges.json",
		PostgresDSN:    "postgres://labd@localhost/labs",
		MetricsFlushS:  10,
		SiteHost:       "https://labs.example.com",
	}
}

// Load reads path, applies defaults for absent keys, applies environment overrides,
// and validates. Unknown keys are an error so typos do not silently fall back.
func Load(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		return Config{}, err
	}
	// A relative challenges_file is relative to the config file, not to the working directory.
	if cfg.ChallengesFile != "" && !filepath.IsAbs(cfg.ChallengesFile) {
		cfg.ChallengesFile = filepath.Join(filepath.Dir(path), cfg.ChallengesFile)
	}
	return cfg, nil
}

// Parse is Load without the file read; used by tests and reload.
func Parse(raw []byte) (Config, error) {
	cfg := Default()
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	// An empty file (io.EOF) means "all defaults".
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyEnv()
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyEnv() {
	if v := os.Getenv(EnvPostgresDSN); v != "" {
		c.PostgresDSN = v
	}
	c.InternalSecret = os.Getenv(EnvInternalSecret)
	c.WSTokenKey = os.Getenv(EnvWSTokenKey)
}

// Validate enforces invariants that must hold before labd binds anything.
func (c Config) Validate() error {
	var errs []error
	if err := requireLoopback("listen_internal", c.ListenInternal); err != nil {
		errs = append(errs, err)
	}
	if err := requireLoopback("listen_ws", c.ListenWS); err != nil {
		errs = append(errs, err)
	}
	if c.ListenInternal == c.ListenWS {
		errs = append(errs, errors.New("listen_internal and listen_ws must differ"))
	}
	if c.MaxSessions < 0 {
		errs = append(errs, errors.New("max_sessions must be >= 0 (0 means drain)"))
	}
	if c.MaxQueue < 0 {
		errs = append(errs, errors.New("max_queue must be >= 0"))
	}
	if c.WSReconnectGraceS <= 0 {
		errs = append(errs, errors.New("ws_reconnect_grace_s must be > 0"))
	}
	if c.MetricsFlushS <= 0 {
		errs = append(errs, errors.New("metrics_flush_s must be > 0"))
	}
	if u, err := url.Parse(c.SiteHost); err != nil || (u.Scheme != "https" && u.Scheme != "http") ||
		u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		errs = append(errs, fmt.Errorf("site_host %q must be an origin like https://labs.example.com", c.SiteHost))
	}
	if strings.TrimSpace(c.Runtime) == "" {
		errs = append(errs, errors.New("runtime must be set"))
	}
	l := c.DefaultLimits
	for name, v := range map[string]int{
		"memory_mb": l.MemoryMB, "cpu_millicores": l.CPUMillicores, "pids": l.Pids,
		"ttl_minutes": l.TTLMinutes, "idle_minutes": l.IdleMinutes, "extend_minutes": l.ExtendMinutes,
	} {
		if v <= 0 {
			errs = append(errs, fmt.Errorf("default_limits.%s must be > 0", name))
		}
	}
	return errors.Join(errs...)
}

func requireLoopback(field, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%s %q: %w", field, addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%s %q must be a loopback address (S11)", field, addr)
	}
	return nil
}

// WSReconnectGrace and MetricsFlush as durations.
func (c Config) WSReconnectGrace() time.Duration {
	return time.Duration(c.WSReconnectGraceS) * time.Second
}
func (c Config) MetricsFlush() time.Duration { return time.Duration(c.MetricsFlushS) * time.Second }
