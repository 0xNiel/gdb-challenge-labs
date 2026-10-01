package orch

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"gdblabs/labd/internal/config"
)

var update = flag.Bool("update", false, "rewrite golden files")

const basePath = "../../sandbox/sandbox-base.json"

func loadBase(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(basePath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func defaultParams() SpecParams {
	return SpecParams{
		ID:     "golden-1",
		Cwd:    "/opt/lab", // a lab image's WORKDIR (ADR 0014)
		Limits: config.Default().DefaultLimits,
		Annotations: map[string]string{
			"lab.session_id": "00000000-0000-0000-0000-000000000001",
			"lab.challenge":  "tier1-01-off-by-one",
		},
	}
}

// TestBuildSpec_Golden pins the exact spec produced for the default limits. Regenerate with
// `go test ./internal/orch -run Golden -update` and review the diff: it is the sandbox.
func TestBuildSpec_Golden(t *testing.T) {
	s, err := BuildSpec(loadBase(t), defaultParams())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.MarshalIndent(s, "", "  ")
	got = append(got, '\n')
	golden := filepath.Join("testdata", "spec_golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("spec differs from %s; if intended, run with -update and review the diff\n--- got\n%s", golden, got)
	}
}

// TestGoldenSatisfiesInvariants protects the invariants even if someone regenerates the golden.
func TestGoldenSatisfiesInvariants(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "spec_golden.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s specs.Spec
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if err := CheckInvariants(&s); err != nil {
		t.Fatalf("golden spec violates invariants: %v", err)
	}
}

func TestBuildSpec_AppliesLimits(t *testing.T) {
	p := defaultParams()
	p.Limits = config.Limits{MemoryMB: 256, CPUMillicores: 250, Pids: 16, TTLMinutes: 1, IdleMinutes: 1, ExtendMinutes: 1}
	p.Args = []string{"/usr/bin/gdb", "-q"}
	s, err := BuildSpec(loadBase(t), p)
	if err != nil {
		t.Fatal(err)
	}
	r := s.Linux.Resources
	if *r.Memory.Limit != 256<<20 || *r.Memory.Swap != 256<<20 {
		t.Errorf("memory %d swap %d", *r.Memory.Limit, *r.Memory.Swap)
	}
	if *r.CPU.Quota != 25000 || *r.CPU.Period != 100000 {
		t.Errorf("cpu quota %d period %d, want 25000/100000", *r.CPU.Quota, *r.CPU.Period)
	}
	// runsc (the default): the lab limit is RLIMIT_NPROC; the cgroup gets gVisor headroom.
	if got := *r.Pids.Limit; got != int64(16+RunscHostPidsOverhead) {
		t.Errorf("runsc cgroup pids %d, want %d", got, 16+RunscHostPidsOverhead)
	}
	// runc: the cgroup is the lab limit and RLIMIT_NPROC is gone (it would count host uid 1000).
	p.Runtime = RuntimeRunc
	s3, err := BuildSpec(loadBase(t), p)
	if err != nil {
		t.Fatal(err)
	}
	if got := *s3.Linux.Resources.Pids.Limit; got != 16 {
		t.Errorf("runc cgroup pids %d, want 16", got)
	}
	for _, rl := range s3.Process.Rlimits {
		if rl.Type == "RLIMIT_NPROC" {
			t.Errorf("runc spec must not set RLIMIT_NPROC, has %d", rl.Hard)
		}
	}
	p.Runtime = "kata"
	if _, err := BuildSpec(loadBase(t), p); err == nil {
		t.Error("unknown runtime accepted")
	}
	nproc := false
	for _, rl := range s.Process.Rlimits {
		if rl.Type == "RLIMIT_NPROC" {
			nproc = true
			if rl.Hard != 16 {
				t.Errorf("RLIMIT_NPROC %d, want 16", rl.Hard)
			}
		}
	}
	if !nproc {
		t.Error("runsc spec lacks RLIMIT_NPROC")
	}
	if s.Linux.CgroupsPath != "/labs/golden-1" {
		t.Errorf("cgroupsPath %q", s.Linux.CgroupsPath)
	}
	if strings.Join(s.Process.Args, " ") != "/usr/bin/gdb -q" {
		t.Errorf("args %v", s.Process.Args)
	}
	// The base must not be mutated between builds (the args slice is cloned).
	s2, _ := BuildSpec(loadBase(t), defaultParams())
	if s2.Process.Args[0] != "/bin/sh" {
		t.Errorf("default args lost: %v", s2.Process.Args)
	}
}

// The lab starts in the image's WORKDIR, else the base spec's /home/lab (ADR 0014). HOME and
// everything else stay as they are.
func TestBuildSpec_Cwd(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ cwd, want string }{
		{"", "/home/lab"},          // an image with no WORKDIR
		{"/opt/lab", "/opt/lab"},   // a lab image
		{"/home/lab", "/home/lab"}, // labbase and perf
	} {
		p := defaultParams()
		p.Cwd = tc.cwd
		s, err := BuildSpec(loadBase(t), p)
		if err != nil {
			t.Fatal(err)
		}
		if s.Process.Cwd != tc.want {
			t.Errorf("Cwd %q: spec cwd %q, want %q", tc.cwd, s.Process.Cwd, tc.want)
		}
		if !slices.Contains(s.Process.Env, "HOME=/home/lab") {
			t.Errorf("Cwd %q: HOME changed: %v", tc.cwd, s.Process.Env)
		}
	}
}

func TestBuildSpec_RejectsBadInput(t *testing.T) {
	base := loadBase(t)
	for name, p := range map[string]SpecParams{
		"empty id":     {ID: "", Limits: config.Default().DefaultLimits},
		"slash in id":  {ID: "../x", Limits: config.Default().DefaultLimits},
		"zero memory":  {ID: "a", Limits: config.Limits{CPUMillicores: 1, Pids: 1}},
		"zero pids":    {ID: "a", Limits: config.Limits{MemoryMB: 1, CPUMillicores: 1}},
		"relative cwd": {ID: "a", Cwd: "opt/lab", Limits: config.Default().DefaultLimits},
		"unclean cwd":  {ID: "a", Cwd: "/opt/../etc", Limits: config.Default().DefaultLimits},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := BuildSpec(base, p); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	if _, err := BuildSpec([]byte("{not json"), defaultParams()); err == nil {
		t.Fatal("bad JSON accepted")
	}
}

// Each mutation of the base spec must be caught by BuildSpec, i.e. by CheckInvariants.
func TestBuildSpec_RefusesLoosenedBase(t *testing.T) {
	cases := map[string]struct {
		mutate  func(*specs.Spec)
		wantErr string
	}{
		"writable root": {func(s *specs.Spec) { s.Root.Readonly = false }, "S3"},
		"root user":     {func(s *specs.Spec) { s.Process.User.UID = 0 }, "S5"},
		"new privs":     {func(s *specs.Spec) { s.Process.NoNewPrivileges = false }, "S5"},
		"a capability": {func(s *specs.Spec) {
			s.Process.Capabilities.Bounding = []string{"CAP_SYS_PTRACE"}
		}, "S6"},
		"host network": {func(s *specs.Spec) {
			for i := range s.Linux.Namespaces {
				if s.Linux.Namespaces[i].Type == specs.NetworkNamespace {
					s.Linux.Namespaces[i].Path = "/proc/1/ns/net"
				}
			}
		}, "S2"},
		"no network ns": {func(s *specs.Spec) {
			var keep []specs.LinuxNamespace
			for _, n := range s.Linux.Namespaces {
				if n.Type != specs.NetworkNamespace {
					keep = append(keep, n)
				}
			}
			s.Linux.Namespaces = keep
		}, "S2"},
		"bind mount": {func(s *specs.Spec) {
			s.Mounts = append(s.Mounts, specs.Mount{Destination: "/data", Type: "bind", Source: "/", Options: []string{"rbind"}})
		}, "S8"},
		"sysfs": {func(s *specs.Spec) {
			s.Mounts = append(s.Mounts, specs.Mount{Destination: "/sys", Type: "sysfs", Source: "sysfs"})
		}, "S8"},
		"exec tmp": {func(s *specs.Spec) {
			for i := range s.Mounts {
				if s.Mounts[i].Destination == "/tmp" {
					s.Mounts[i].Options = []string{"nosuid", "nodev", "size=16m"}
				}
			}
		}, "S4"},
		"unbounded tmp": {func(s *specs.Spec) {
			for i := range s.Mounts {
				if s.Mounts[i].Destination == "/home/lab" {
					s.Mounts[i].Options = []string{"nosuid", "nodev", "noexec"}
				}
			}
		}, "S4"},
		"big fsize": {func(s *specs.Spec) {
			for i := range s.Process.Rlimits {
				if s.Process.Rlimits[i].Type == "RLIMIT_FSIZE" {
					s.Process.Rlimits[i].Hard = 1 << 40
				}
			}
		}, "RLIMIT_FSIZE"},
		"device allowed": {func(s *specs.Spec) {
			s.Linux.Resources.Devices = []specs.LinuxDeviceCgroup{{Allow: true, Access: "rwm"}}
		}, "S8"},
		"no terminal": {func(s *specs.Spec) { s.Process.Terminal = false }, "ADR 0007"},
		"no nproc": {func(s *specs.Spec) {
			var keep []specs.POSIXRlimit
			for _, r := range s.Process.Rlimits {
				if r.Type != "RLIMIT_NPROC" {
					keep = append(keep, r)
				}
			}
			s.Process.Rlimits = keep
		}, "RLIMIT_NPROC"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var s specs.Spec
			if err := json.Unmarshal(loadBase(t), &s); err != nil {
				t.Fatal(err)
			}
			tc.mutate(&s)
			raw, _ := json.Marshal(&s)
			_, err := BuildSpec(raw, defaultParams())
			if err == nil {
				t.Fatalf("loosened base accepted")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}
