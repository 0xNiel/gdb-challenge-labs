package perf

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// A capacity search (Phase 7, task 7.11; ADR 0017) is P10 run at increasing counts, one
// run file per count: run-P10-<date>-<host>-n<count>.json (labd/perf/capacity.sh).

var p10Name = regexp.MustCompile(`^run-P10-(\d{4}-\d{2}-\d{2})-(.+)-n(\d+)\.json$`)

// CapacityStep is one count's result.
type CapacityStep struct {
	N            int       `json:"n"`
	File         string    `json:"file"`
	Pass         bool      `json:"pass"`
	Missed       []string  `json:"missed,omitempty"`
	LabStartMS   Pctl      `json:"lab_start_ms"`    // learners: request to the shell's prompt
	EchoMS       Pctl      `json:"echo_ms"`         // learners
	NextMS       Pctl      `json:"next_ms"`         // send to gdb's prompt
	GDBStartMS   Pctl      `json:"gdb_start_ms"`    // `gdb -q ./<entry>` to its prompt
	RunMS        Pctl      `json:"run_ms"`          // gdb `run` to the next prompt
	ContinueMS   Pctl      `json:"continue_ms"`     // includes software watchpoints
	ShellRunMS   Pctl      `json:"shell_run_ms"`    // the program run plainly from the shell
	LearnerCPU   float64   `json:"learner_cpu_pct"` // % of one core, mean over learner labs
	AbuserCPU    float64   `json:"abuser_cpu_pct"`
	HostCPU      P50P95Max `json:"host_cpu_pct"` // % of all online CPUs
	PSICPU       float64   `json:"psi_cpu_some_avg10_max"`
	LabMemMB     Pctl      `json:"lab_mem_mb"`
	HostPerLabMB float64   `json:"host_per_lab_mb"`
	HostMemMaxMB float64   `json:"host_mem_used_max_mb"`
	Commands     int       `json:"commands"`
	CmdErrors    int       `json:"command_errors"`
	OOMKills     int64     `json:"oom_kills"`
}

// CapacitySearch is the whole search for one host.
type CapacitySearch struct {
	Host       string         `json:"host"`
	Date       string         `json:"date"`
	Arch       string         `json:"arch"`
	CPUs       int            `json:"cpus"` // online CPUs during the runs (8 to stand in for the VPS)
	MemMB      float64        `json:"host_mem_total_mb"`
	Runtime    string         `json:"runtime"`
	Mix        map[string]int `json:"mix"`
	HoldS      int            `json:"hold_s"`
	Steps      []CapacityStep `json:"steps"`
	MaxPassing int            `json:"max_passing"` // largest count with every criterion met (0: none)
	FirstFail  int            `json:"first_failing,omitempty"`
}

// LoadCapacitySearch reads the newest P10 run at each count for host in dir.
func LoadCapacitySearch(dir, host string) (CapacitySearch, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return CapacitySearch{}, err
	}
	newest := map[int]RunFile{}
	files := map[int]string{}
	for _, e := range es {
		m := p10Name.FindStringSubmatch(e.Name())
		if m == nil || m[2] != host {
			continue
		}
		n, _ := strconv.Atoi(m[3])
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return CapacitySearch{}, err
		}
		var rf RunFile
		if err := json.Unmarshal(b, &rf); err != nil {
			return CapacitySearch{}, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if old, ok := newest[n]; ok && old.Meta.Date > rf.Meta.Date {
			continue
		}
		newest[n], files[n] = rf, e.Name()
	}
	if len(newest) == 0 {
		return CapacitySearch{}, fmt.Errorf("no run-P10-*-%s-n<count>.json in %s", host, dir)
	}
	return BuildCapacitySearch(host, newest, files), nil
}

// BuildCapacitySearch turns one run per count into the search's rows.
func BuildCapacitySearch(host string, runs map[int]RunFile, files map[int]string) CapacitySearch {
	ns := make([]int, 0, len(runs))
	for n := range runs {
		ns = append(ns, n)
	}
	slices.Sort(ns)
	cs := CapacitySearch{Host: host}
	for i, n := range ns {
		rf := runs[n]
		if i == 0 || rf.Meta.Date > cs.Date {
			cs.Date = rf.Meta.Date
		}
		cs.Arch, cs.CPUs, cs.MemMB, cs.Runtime, cs.Mix, cs.HoldS =
			rf.Meta.Arch, rf.Meta.CPUs, rf.Meta.HostMemMB, rf.Meta.Runtime, rf.Meta.Mix, rf.Meta.HoldS
		s := rf.Summary
		st := CapacityStep{
			N: n, File: files[n], Pass: len(rf.Criteria) > 0,
			LabStartMS: s.StartByProfile[Learner], EchoMS: s.EchoMSByProfile[Learner],
			NextMS: s.CmdMS["next"], GDBStartMS: s.CmdMS["gdb_start"], RunMS: s.CmdMS["run"],
			ContinueMS: s.CmdMS["continue"], ShellRunMS: s.CmdMS["shell_run"],
			LearnerCPU: s.LabCPUPctByProfile[Learner], AbuserCPU: s.LabCPUPctByProfile[Abuser],
			HostCPU: s.HostCPUPct, PSICPU: s.PSIMax.CPU, LabMemMB: s.LabMemMB,
			HostPerLabMB: s.HostPerLabMB, HostMemMaxMB: s.HostMemUsedMB.Max,
			Commands: s.Commands, CmdErrors: s.CommandErrors, OOMKills: s.OOMKills,
		}
		for _, c := range rf.Criteria {
			if !c.Pass {
				st.Pass = false
				st.Missed = append(st.Missed, c.Criterion+": "+c.Measured)
			}
		}
		if st.Pass && cs.FirstFail == 0 {
			cs.MaxPassing = n
		}
		if !st.Pass && cs.FirstFail == 0 {
			cs.FirstFail = n
		}
		cs.Steps = append(cs.Steps, st)
	}
	if len(cs.Date) >= 10 {
		cs.Date = cs.Date[:10]
	}
	return cs
}

// RenderCapacitySearch is the Markdown record.
func RenderCapacitySearch(cs CapacitySearch) string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	rt := strings.TrimSuffix(strings.TrimPrefix(cs.Runtime, "io.containerd."), ".v1")
	w("# Capacity search — %s, %s\n\n", cs.Host, cs.Date)
	w("P10 (Phase 7, task 7.11; ADR 0017) on `%s`: %s, **%d CPUs online**, %.0f MB RAM, labs under %s. ", cs.Host, cs.Arch, cs.CPUs, cs.MemMB, rt)
	w("Mix %s, %d s hold at each count. Generated by `labd-perf capacity` from the run files named below.\n\n", mixString(cs.Mix), cs.HoldS)
	if cs.MaxPassing > 0 {
		w("**Largest count that met every criterion: %d.**", cs.MaxPassing)
	} else {
		w("**No count met every criterion.**")
	}
	if cs.FirstFail > 0 {
		w(" The first to miss was %d.", cs.FirstFail)
	}
	w("\n\nCriteria: learners' lab start p95 < 2 s (request to the shell's prompt), learners' echo p95 < 100 ms, `next` p95 < 250 ms, no command timed out, no OOM kill, every simulated user completed.\n\n")
	w("| Labs | Result | Lab start p95 | Echo p95 | `next` p95 | gdb start p95 | `run` p95 | `continue` p95 | Learner CPU | Abuser CPU | Host CPU p95 | CPU pressure max | Lab mem p95 | Host per lab |\n")
	w("| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, s := range cs.Steps {
		res := "pass"
		if !s.Pass {
			res = "**miss**"
		}
		w("| %d | %s | %.0f ms | %.1f ms | %.0f ms | %.0f ms | %.0f ms | %.0f ms | %.2f %% | %.1f %% | %.0f %% | %.1f | %.1f MiB | %.1f MB |\n",
			s.N, res, s.LabStartMS.P95, s.EchoMS.P95, s.NextMS.P95, s.GDBStartMS.P95, s.RunMS.P95, s.ContinueMS.P95,
			s.LearnerCPU, s.AbuserCPU, s.HostCPU.P95, s.PSICPU, s.LabMemMB.P95, s.HostPerLabMB)
	}
	w("\nCPU columns: learner and abuser are %% of one core per lab, averaged; host is %% of all online CPUs. Pressure is `/proc/pressure/cpu` some avg10.\n")
	var missed []string
	for _, s := range cs.Steps {
		for _, m := range s.Missed {
			missed = append(missed, fmt.Sprintf("- %d labs: %s", s.N, m))
		}
	}
	if len(missed) > 0 {
		w("\nMissed criteria:\n\n%s\n", strings.Join(missed, "\n"))
	}
	w("\nSources:\n\n")
	for _, s := range cs.Steps {
		w("- %d labs: `%s` (%d commands, %d timed out)\n", s.N, s.File, s.Commands, s.CmdErrors)
	}
	return b.String()
}

func mixString(mix map[string]int) string {
	var parts []string
	for _, name := range []string{Learner, Reader, Stepper, Abuser} {
		if mix[name] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", name, mix[name]))
		}
	}
	return strings.Join(parts, ", ")
}
