// Package orch owns lab sessions and everything that touches containerd (Phase 2).
// In Phase 1 it holds the sandbox spec builder and a one-shot runner used by the P0 checks.
package orch

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	specs "github.com/opencontainers/runtime-spec/specs-go"

	"gdblabs/labd/internal/config"
)

// CgroupParent is the cgroup v2 subtree every lab lives in: /sys/fs/cgroup/labs/<id>.
// The metrics sampler (Phase 7) and the perf collector read memory.current and cpu.stat there.
const CgroupParent = "/labs"

// cpuPeriodUS is the CFS period; the quota is derived from millicores against it.
const cpuPeriodUS = 100000

// SpecParams are the per-session inputs merged into the base spec.
type SpecParams struct {
	ID          string            // container id; also the cgroup leaf name
	Args        []string          // process args; nil keeps the base spec's (/bin/sh)
	Limits      config.Limits     // manifest limits; zero fields must be filled by the caller
	Annotations map[string]string // lab.session_id, lab.user_id, ... (spec: container labels)
	// HostPidsOverhead is added to Limits.Pids for the cgroup pids.max. Under gVisor the cgroup
	// also counts the sandbox's own host processes and threads (Sentry, gofer, systrap stubs),
	// so the lab's own limit is enforced with RLIMIT_NPROC and the cgroup gets headroom.
	// See RunscHostPidsOverhead and docs/decisions/0009-pids-limit-under-gvisor.md.
	HostPidsOverhead int
}

// RunscHostPidsOverhead is the cgroup pids headroom for gVisor's own host tasks (ADR 0009).
// Measured on the arm64 dev VM, 2026-09-28: an idle sandbox uses 20 host tasks; a fork storm
// that reaches the lab's RLIMIT_NPROC of 32 (30 children) peaks at 85. 96 over the lab's 32
// gives a cgroup limit of 128. Re-check on x86-64 in Phase 1 and under load in Phase 4.
const RunscHostPidsOverhead = 96

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

// BuildSpec parses the base spec (labd/sandbox/sandbox-base.json), applies p, and returns the
// result only if it still satisfies every sandbox invariant (CheckInvariants).
func BuildSpec(base []byte, p SpecParams) (*specs.Spec, error) {
	if !idPattern.MatchString(p.ID) {
		return nil, fmt.Errorf("invalid container id %q", p.ID)
	}
	var s specs.Spec
	if err := json.Unmarshal(base, &s); err != nil {
		return nil, fmt.Errorf("parse base spec: %w", err)
	}
	if s.Process == nil || s.Linux == nil || s.Linux.Resources == nil {
		return nil, errors.New("base spec lacks process, linux or linux.resources")
	}
	l := p.Limits
	if l.MemoryMB <= 0 || l.CPUMillicores <= 0 || l.Pids <= 0 {
		return nil, fmt.Errorf("limits must be positive: %+v", l)
	}

	if p.Args != nil {
		s.Process.Args = slices.Clone(p.Args)
	}

	mem := int64(l.MemoryMB) * 1024 * 1024
	swap := mem // limit == memory+swap limit: no swap (spec: "no swap")
	s.Linux.Resources.Memory = &specs.LinuxMemory{Limit: &mem, Swap: &swap}
	quota := int64(l.CPUMillicores) * cpuPeriodUS / 1000
	period := uint64(cpuPeriodUS)
	s.Linux.Resources.CPU = &specs.LinuxCPU{Quota: &quota, Period: &period}
	if p.HostPidsOverhead < 0 {
		return nil, fmt.Errorf("negative HostPidsOverhead %d", p.HostPidsOverhead)
	}
	pids := int64(l.Pids + p.HostPidsOverhead)
	s.Linux.Resources.Pids = &specs.LinuxPids{Limit: &pids}
	for i, r := range s.Process.Rlimits {
		if r.Type == "RLIMIT_NPROC" {
			s.Process.Rlimits[i].Hard, s.Process.Rlimits[i].Soft = uint64(l.Pids), uint64(l.Pids)
		}
	}

	s.Linux.CgroupsPath = CgroupParent + "/" + p.ID
	if len(p.Annotations) > 0 {
		if s.Annotations == nil {
			s.Annotations = map[string]string{}
		}
		for k, v := range p.Annotations {
			s.Annotations[k] = v
		}
	}

	if err := CheckInvariants(&s); err != nil {
		return nil, fmt.Errorf("sandbox spec violates invariants: %w", err)
	}
	return &s, nil
}

// allowedMounts is the complete list of mounts a lab may have (S4, S8). Anything else, and in
// particular any bind mount, is a violation.
var allowedMounts = map[string]string{
	"/proc":     "proc",
	"/dev":      "tmpfs",
	"/dev/pts":  "devpts",
	"/tmp":      "tmpfs",
	"/home/lab": "tmpfs",
}

// CheckInvariants enforces docs/SECURITY-INVARIANTS.md S2–S8 on a spec. It is structural on
// purpose: a regenerated golden file cannot loosen the sandbox without this failing.
func CheckInvariants(s *specs.Spec) error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if s.Process == nil || s.Linux == nil || s.Root == nil {
		return errors.New("spec lacks process, linux or root")
	}

	// ADR 0007: every lab has a terminal.
	if !s.Process.Terminal {
		bad("process.terminal must be true (ADR 0007)")
	}

	// S2: a fresh network namespace (no path) — no interfaces from the host.
	netNS := false
	for _, ns := range s.Linux.Namespaces {
		if ns.Type == specs.NetworkNamespace {
			if ns.Path != "" {
				bad("S2: network namespace must be new, not joined (%s)", ns.Path)
			}
			netNS = true
		}
	}
	if !netNS {
		bad("S2: no network namespace")
	}
	for _, want := range []specs.LinuxNamespaceType{specs.PIDNamespace, specs.MountNamespace, specs.IPCNamespace, specs.UTSNamespace} {
		if !slices.ContainsFunc(s.Linux.Namespaces, func(n specs.LinuxNamespace) bool { return n.Type == want && n.Path == "" }) {
			bad("namespace %s missing or joined", want)
		}
	}

	// S3: read-only root.
	if !s.Root.Readonly {
		bad("S3: root must be read-only")
	}

	// S4 + S8: exact mount set; no binds; tmpfs writable areas are noexec,nosuid,nodev and sized.
	seen := map[string]bool{}
	for _, m := range s.Mounts {
		wantType, ok := allowedMounts[m.Destination]
		if !ok {
			bad("S8: unexpected mount %s (%s)", m.Destination, m.Type)
			continue
		}
		if m.Type != wantType {
			bad("S8: mount %s has type %s, want %s", m.Destination, m.Type, wantType)
		}
		if slices.Contains(m.Options, "bind") || slices.Contains(m.Options, "rbind") {
			bad("S8: bind mount at %s", m.Destination)
		}
		seen[m.Destination] = true
		if m.Destination == "/tmp" || m.Destination == "/home/lab" {
			for _, o := range []string{"noexec", "nosuid", "nodev"} {
				if !slices.Contains(m.Options, o) {
					bad("S4: %s lacks %s", m.Destination, o)
				}
			}
			if !slices.ContainsFunc(m.Options, func(o string) bool { return strings.HasPrefix(o, "size=") }) {
				bad("S4: %s has no size limit", m.Destination)
			}
		}
	}
	for dst := range allowedMounts {
		if !seen[dst] {
			bad("mount %s missing", dst)
		}
	}

	// S5: non-root, no new privileges.
	if s.Process.User.UID == 0 || s.Process.User.GID == 0 {
		bad("S5: process must not run as uid/gid 0")
	}
	if !s.Process.NoNewPrivileges {
		bad("S5: noNewPrivileges must be true")
	}

	// S6: no capabilities at all.
	if c := s.Process.Capabilities; c != nil {
		for name, set := range map[string][]string{
			"bounding": c.Bounding, "effective": c.Effective, "inheritable": c.Inheritable,
			"permitted": c.Permitted, "ambient": c.Ambient,
		} {
			if len(set) > 0 {
				bad("S6: capabilities.%s must be empty, has %v", name, set)
			}
		}
	}

	// S7: resource limits present.
	r := s.Linux.Resources
	switch {
	case r == nil:
		bad("S7: no linux.resources")
	default:
		if r.Memory == nil || r.Memory.Limit == nil || *r.Memory.Limit <= 0 {
			bad("S7: memory limit missing")
		} else if r.Memory.Swap == nil || *r.Memory.Swap != *r.Memory.Limit {
			bad("S7: swap must equal the memory limit (no swap)")
		}
		if r.CPU == nil || r.CPU.Quota == nil || *r.CPU.Quota <= 0 {
			bad("S7: cpu quota missing")
		}
		if r.Pids == nil || r.Pids.Limit == nil || *r.Pids.Limit <= 0 || *r.Pids.Limit > 1024 {
			bad("S7: pids limit missing or unreasonable")
		}
	}
	rl := map[string]uint64{}
	for _, x := range s.Process.Rlimits {
		rl[x.Type] = x.Hard
	}
	if v, ok := rl["RLIMIT_FSIZE"]; !ok || v == 0 || v > 32<<20 {
		bad("S7: RLIMIT_FSIZE must be set and <= 32 MiB")
	}
	if v, ok := rl["RLIMIT_NOFILE"]; !ok || v == 0 || v > 256 {
		bad("S7: RLIMIT_NOFILE must be set and <= 256")
	}
	if v, ok := rl["RLIMIT_NPROC"]; !ok || v == 0 || v > 256 {
		bad("S7: RLIMIT_NPROC must be set and <= 256 (the lab's process limit)")
	} else if r != nil && r.Pids != nil && r.Pids.Limit != nil && int64(v) > *r.Pids.Limit {
		bad("S7: RLIMIT_NPROC %d exceeds the cgroup pids limit %d", v, *r.Pids.Limit)
	}

	// S8: no devices allowed beyond what the runtime provides for the PTY.
	if r != nil {
		for _, d := range r.Devices {
			if d.Allow {
				bad("S8: device rule allows access: %+v", d)
			}
		}
	}
	if len(s.Linux.Devices) > 0 {
		bad("S8: linux.devices must be empty")
	}
	return errors.Join(errs...)
}
