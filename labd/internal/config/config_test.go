package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse_EmptyFileUsesDefaults(t *testing.T) {
	t.Setenv(EnvPostgresDSN, "")
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(empty): %v", err)
	}
	want := Default()
	if cfg.MaxSessions != want.MaxSessions || cfg.MaxQueue != want.MaxQueue ||
		cfg.ListenInternal != want.ListenInternal || cfg.DefaultLimits != want.DefaultLimits {
		t.Fatalf("defaults not applied: got %+v", cfg)
	}
}

func TestParse_PartialOverrideKeepsOtherDefaults(t *testing.T) {
	cfg, err := Parse([]byte("max_sessions: 20\ndefault_limits: {memory_mb: 256}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxSessions != 20 {
		t.Errorf("max_sessions = %d, want 20", cfg.MaxSessions)
	}
	if cfg.DefaultLimits.MemoryMB != 256 {
		t.Errorf("memory_mb = %d, want 256", cfg.DefaultLimits.MemoryMB)
	}
	if cfg.DefaultLimits.Pids != 32 || cfg.DefaultLimits.TTLMinutes != 60 {
		t.Errorf("other limits lost their defaults: %+v", cfg.DefaultLimits)
	}
	if cfg.MaxQueue != 50 {
		t.Errorf("max_queue = %d, want default 50", cfg.MaxQueue)
	}
}

func TestParse_SpecExampleFileLoads(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "labd.example.yaml"))
	if err != nil {
		t.Fatalf("labd.example.yaml must load: %v", err)
	}
	if cfg.Runtime != "io.containerd.runsc.v1" {
		t.Errorf("runtime = %q", cfg.Runtime)
	}
}

// deploy/labd.prod.yaml is what the VPS runs: gVisor only (S1), no dev switches, and the
// capacity ADR 0013 chose.
func TestParse_ProductionFile(t *testing.T) {
	cfg, err := Load(filepath.Join("..", "..", "..", "deploy", "labd.prod.yaml"))
	if err != nil {
		t.Fatalf("deploy/labd.prod.yaml must load: %v", err)
	}
	if cfg.Runtime != "io.containerd.runsc.v1" {
		t.Errorf("runtime = %q, want runsc only (S1)", cfg.Runtime)
	}
	if cfg.DevAllowNoOrigin || cfg.DevTestpage {
		t.Error("a dev switch is on in the production config")
	}
	if cfg.MaxSessions != 100 || cfg.MaxQueue != 50 {
		t.Errorf("max_sessions %d, max_queue %d; ADR 0013 says 100 and 50", cfg.MaxSessions, cfg.MaxQueue)
	}
}

func TestParse_DrainModeAccepted(t *testing.T) {
	cfg, err := Parse([]byte("max_sessions: 0\n"))
	if err != nil {
		t.Fatalf("max_sessions: 0 must be accepted (drain): %v", err)
	}
	if cfg.MaxSessions != 0 {
		t.Fatalf("max_sessions = %d", cfg.MaxSessions)
	}
}

func TestParse_Rejects(t *testing.T) {
	cases := map[string]struct {
		yaml, wantErr string
	}{
		"non-loopback internal": {"listen_internal: 0.0.0.0:8081\n", "loopback"},
		"public ws":             {"listen_ws: 10.0.0.5:8082\n", "loopback"},
		"hostname not loopback": {"listen_internal: example.com:8081\n", "loopback"},
		"missing port":          {"listen_internal: 127.0.0.1\n", "listen_internal"},
		"same listeners":        {"listen_internal: 127.0.0.1:9000\nlisten_ws: 127.0.0.1:9000\n", "must differ"},
		"negative sessions":     {"max_sessions: -1\n", "max_sessions"},
		"negative queue":        {"max_queue: -1\n", "max_queue"},
		"zero grace":            {"ws_reconnect_grace_s: 0\n", "ws_reconnect_grace_s"},
		"zero limit":            {"default_limits: {pids: 0}\n", "default_limits.pids"},
		"unknown key":           {"max_sesions: 10\n", "max_sesions"},
		"empty runtime":         {"runtime: \"\"\n", "runtime"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestParse_LoopbackVariantsAccepted(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:1", "[::1]:1", "localhost:1", "127.0.0.2:1"} {
		if _, err := Parse([]byte("listen_internal: \"" + addr + "\"\n")); err != nil {
			t.Errorf("%s rejected: %v", addr, err)
		}
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err == nil || !os.IsNotExist(unwrapAll(err)) {
		t.Fatalf("want not-exist error, got %v", err)
	}
}

func TestEnvOverrides(t *testing.T) {
	t.Setenv(EnvPostgresDSN, "postgres://override@127.0.0.1/x")
	t.Setenv(EnvInternalSecret, "s3cret")
	cfg, err := Parse([]byte("postgres_dsn: postgres://file@localhost/labs\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://override@127.0.0.1/x" {
		t.Errorf("DSN env override not applied: %q", cfg.PostgresDSN)
	}
	if cfg.InternalSecret != "s3cret" {
		t.Errorf("internal secret not read from env")
	}
}

func TestSecretNeverFromFile(t *testing.T) {
	t.Setenv(EnvInternalSecret, "")
	// The field is yaml:"-"; with KnownFields a key of that name is rejected outright.
	if _, err := Parse([]byte("internal_secret: leaked\n")); err == nil {
		t.Fatal("a secret key in the file must be rejected")
	}
}

func unwrapAll(err error) error {
	for {
		u, ok := err.(interface{ Unwrap() error })
		if !ok || u.Unwrap() == nil {
			return err
		}
		err = u.Unwrap()
	}
}

func TestLoad_RelativeChallengesFile(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]string{
		"challenges_file: challenges.json\n":      filepath.Join(dir, "challenges.json"),
		"challenges_file: /etc/labd/c.json\n":     "/etc/labd/c.json",
		"challenges_file: ../x/challenges.json\n": filepath.Join(filepath.Dir(dir), "x", "challenges.json"),
	} {
		p := filepath.Join(dir, "labd.yaml")
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg, err := Load(p)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.ChallengesFile != want {
			t.Errorf("%q: got %s, want %s", name, cfg.ChallengesFile, want)
		}
	}
}

func TestSiteHost(t *testing.T) {
	for v, ok := range map[string]bool{
		"https://labs.example.com": true, "http://127.0.0.1:8082": true, "https://labs.example.com/": true,
		"labs.example.com": false, "ftp://x": false, "https://x/path": false, "https://": false, "https://x?a=1": false,
	} {
		_, err := Parse([]byte("site_host: " + v + "\n"))
		if (err == nil) != ok {
			t.Errorf("site_host %q: err %v, want ok=%v", v, err, ok)
		}
	}
}
