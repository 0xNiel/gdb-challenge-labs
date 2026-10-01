package perf

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// LoadedRun is a run file and where it came from.
type LoadedRun struct {
	File string
	Run  RunFile
}

var runName = regexp.MustCompile(`^run-(P[1-9])-(\d{4}-\d{2}-\d{2})-(.+)\.json$`)

// LoadRuns reads dir's run files for host and keeps the newest of each scenario and label,
// keyed "P2", "P1-runc", "P2-kvm", ...
func LoadRuns(dir, host string) (map[string]LoadedRun, error) {
	es, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]LoadedRun{}
	for _, e := range es {
		m := runName.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		key, rest := m[1], m[3]
		if rest != host {
			label, ok := strings.CutPrefix(rest, host+"-")
			if !ok {
				continue
			}
			key += "-" + label
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var rf RunFile
		if err := json.Unmarshal(b, &rf); err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		if old, ok := out[key]; ok && old.Run.Meta.Date > rf.Meta.Date {
			continue
		}
		out[key] = LoadedRun{File: e.Name(), Run: rf}
	}
	return out, nil
}

// SpecSessions is the spec's target: 100 labs on the 8 vCPU / 32 GB box.
const (
	SpecSessions = 100
	BoxMemMB     = 32768
)

// BuildReport fills the spec's perf-report from the runs, noting each value's source and
// every value it could not fill.
func BuildReport(runs map[string]LoadedRun) (Report, []string) {
	var rep Report
	meta := &ReportMeta{Sources: map[string]string{}}
	rep.Meta = meta
	rep.Extra = map[string]any{}
	var missing []string
	get := func(key, field string) (RunFile, bool) {
		lr, ok := runs[key]
		if !ok {
			missing = append(missing, fmt.Sprintf("%s (needs run %s)", field, key))
			return RunFile{}, false
		}
		meta.Sources[field] = lr.File
		return lr.Run, true
	}
	for _, lr := range runs {
		meta.Host, meta.Arch = lr.Run.Meta.Host, lr.Run.Meta.Arch
		if lr.Run.Meta.Date > meta.Date {
			meta.Date = lr.Run.Meta.Date
		}
	}
	for _, k := range []string{"P2", "P3", "P8"} {
		if lr, ok := runs[k]; ok && (lr.Run.Meta.N < SpecSessions || lr.Run.Meta.Partial != "") {
			meta.Partial = append(meta.Partial, fmt.Sprintf("%s ran at N=%d: %s", k, lr.Run.Meta.N, firstNonEmpty(lr.Run.Meta.Partial, "below the spec's 100")))
		}
	}

	if p2, ok := get("P2", "per_lab.rss_mb"); ok {
		s := p2.Summary
		rep.PerLab.RSSMB = P50P95Max{P50: round(s.LabMemMB.P50, 1), P95: round(s.LabMemMB.P95, 1), Max: round(s.LabMemMB.Max, 1)}
		meta.Sources["per_lab.cpu_pct_avg"] = runs["P2"].File
		rep.PerLab.CPUPctAvg = s.LabCPUPctAvg
		if d, ok := diskPerLab(p2); ok {
			rep.PerLab.DiskMB = d
			meta.Sources["per_lab.disk_mb"] = runs["P2"].File
		} else {
			missing = append(missing, "per_lab.disk_mb (P2 has no disk samples)")
		}
		rep.HostAt100.MemUsedMB = s.HostMemUsedMB.Max
		meta.Sources["host_at_100.mem_used_mb"] = runs["P2"].File
		rep.HostAt100.StartLatencyMS = P50P95{P50: round(s.StartToPromptMS.P50, 0), P95: round(s.StartToPromptMS.P95, 0)}
		meta.Sources["host_at_100.start_latency_ms"] = runs["P2"].File
		rep.GVisorOverhead.SentryRSSMB = round(s.SentryRSSMB.P50, 1)
		meta.Sources["gvisor_overhead.sentry_rss_mb"] = runs["P2"].File

		perLab := math.Max(s.LabMemMB.P95, s.HostPerLabMB)
		if perLab > 0 {
			rep.Derived.MaxSessionsAt25pctHeadroom = int(math.Floor((BoxMemMB*0.75 - s.HostBaselineMB) / perLab))
			meta.Sources["derived.max_sessions_at_25pct_headroom"] = runs["P2"].File
			rep.Extra["derived_inputs"] = map[string]any{
				"box_mb": BoxMemMB, "baseline_mb": s.HostBaselineMB, "per_lab_mb": round(perLab, 1),
				"per_lab_cgroup_p95_mb": round(s.LabMemMB.P95, 1), "per_lab_host_mb": s.HostPerLabMB,
				"note": fmt.Sprintf("baseline is host memory used with 0 labs on %s; per lab is the larger of the cgroup p95 and the host's (used - idle) / labs, which includes each lab's shim", p2.Meta.Host),
			}
		}
		rep.Extra["at_n"] = map[string]any{
			"n": p2.Meta.N, "psi_max": s.PSIMax, "labd_rss_mb": s.LabdRSSMB.Peak, "labd_goroutines": s.LabdGoroutines.Peak,
			"containerd_rss_mb": s.ContainerdRSSMB.Peak, "sentry_rss_mb": s.SentryRSSMB, "lab_host_rss_mb": s.LabHostRSSMB,
			"host_per_lab_mb": s.HostPerLabMB, "host_baseline_mb": s.HostBaselineMB, "oom_kills": s.OOMKills,
		}
	}
	if p3, ok := get("P3", "host_at_100.echo_latency_ms"); ok {
		s := p3.Summary
		rep.HostAt100.EchoLatencyMS = P50P95{P50: round(s.EchoMS.P50, 1), P95: round(s.EchoMS.P95, 1)}
		rep.HostAt100.CPUPct = s.HostCPUPct.P50
		meta.Sources["host_at_100.cpu_pct"] = runs["P3"].File
		rep.Extra["p3"] = map[string]any{"echo_ms_by_profile": s.EchoMSByProfile, "lab_cpu_pct_by_profile": s.LabCPUPctByProfile,
			"psi_max": s.PSIMax, "host_cpu_pct": s.HostCPUPct, "host_mem_used_mb": s.HostMemUsedMB}
	}
	if p8, ok := get("P8", "per_lab.ws_bps_in"); ok {
		rep.PerLab.WSBpsIn, rep.PerLab.WSBpsOut = p8.Summary.WSBpsIn, p8.Summary.WSBpsOut
		meta.Sources["per_lab.ws_bps_out"] = runs["P8"].File
		rep.Extra["bandwidth_aggregate_bps_out"] = p8.Summary.WSBpsOutAggregate
	}
	if p1, ok := get("P1", "gvisor_overhead.step_cmd_ms_runsc"); ok {
		rep.GVisorOverhead.StepCmdMSRunsc = round(p1.Summary.StepCmdMS.P50, 2)
	}
	if p1r, ok := get("P1-runc", "gvisor_overhead.step_cmd_ms_runc"); ok {
		rep.GVisorOverhead.StepCmdMSRunc = round(p1r.Summary.StepCmdMS.P50, 2)
	}
	for _, k := range []string{"P2-runc", "P3-runc", "P1-kvm", "P2-kvm"} {
		if lr, ok := runs[k]; ok {
			s := lr.Run.Summary
			rep.Extra[strings.ToLower(strings.ReplaceAll(k, "-", "_"))] = map[string]any{
				"file": lr.File, "lab_mem_mb": s.LabMemMB, "host_per_lab_mb": s.HostPerLabMB, "echo_ms": s.EchoMS,
				"start_to_prompt_ms": s.StartToPromptMS, "step_cmd_ms": s.StepCmdMS, "lab_cpu_pct_avg": s.LabCPUPctAvg,
			}
		}
	}

	// Leaks: the worst of every run that ends with everything stopped.
	for _, k := range sortedKeys(runs) {
		l := runs[k].Run.Summary.Leaks
		rep.Leaks.Containers = max(rep.Leaks.Containers, l.Containers)
		rep.Leaks.Goroutines = max(rep.Leaks.Goroutines, l.Goroutines)
		rep.Leaks.FIFOs = max(rep.Leaks.FIFOs, l.FIFOs)
	}
	if lr, ok := runs["P5"]; ok {
		rep.Extra["p5"] = map[string]any{"recovered_ms": lr.Run.Extra["recovered_ms"], "down_ms": lr.Run.Extra["down_ms"],
			"adopted_ms": lr.Run.Extra["adopted_ms"], "file": lr.File}
	}
	if lr, ok := runs["P6"]; ok {
		rep.Extra["p6"] = lr.Run.Extra["p6"]
	}
	if lr, ok := runs["P7"]; ok {
		rep.Extra["p7"] = lr.Run.Extra["p7"]
	}
	if lr, ok := runs["P9"]; ok {
		rep.Extra["p9"] = diskGrowth(lr.Run)
	}
	if lr, ok := runs["P4"]; ok {
		rep.Extra["p4"] = map[string]any{"start_to_prompt_ms": lr.Run.Summary.StartToPromptMS, "sessions": lr.Run.Summary.Sessions}
	}
	if img := imageSizes(runs); img != nil {
		rep.Extra["images_mib"] = img
	}
	failed := map[string][]Criterion{}
	for _, k := range sortedKeys(runs) {
		for _, c := range runs[k].Run.Criteria {
			if !c.Pass {
				failed[k] = append(failed[k], c)
			}
		}
	}
	rep.Extra["criteria_missed"] = failed
	slices.Sort(missing)
	return rep, missing
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// diskSamples returns a run's disk.sh samples in order.
func diskSamples(rf RunFile) []map[string]any {
	raw, _ := rf.Extra["disk"].([]any)
	var out []map[string]any
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func num(m map[string]any, keys ...string) (float64, bool) {
	var cur any = m
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return 0, false
		}
		cur = mm[k]
	}
	f, ok := cur.(float64)
	return f, ok
}

// diskPerLab is the snapshot growth per running lab, in MB: the sample with the most labs
// against the first one (before any lab). It uses snapshots_kb when the run has it; runs
// before 2026-10-01 have whole MB only, which rounds a few KB per lab to nothing.
func diskPerLab(rf RunFile) (float64, bool) {
	ds := diskSamples(rf)
	if len(ds) < 2 {
		return 0, false
	}
	key, scale := "snapshots_kb", 1.0/1024
	if _, ok := num(ds[0], key); !ok {
		key, scale = "snapshots_mb", 1
	}
	base, ok := num(ds[0], key)
	if !ok {
		return 0, false
	}
	best, bestN := 0.0, 0.0
	for _, d := range ds[1:] {
		n, _ := num(d, "active")
		v, ok := num(d, key)
		if ok && n > bestN {
			best, bestN = v, n
		}
	}
	if bestN == 0 {
		return 0, false
	}
	return round(math.Max(best-base, 0)*scale/bestN, 4), true
}

// diskGrowth is P9's growth per hour of snapshots, journal and labd's tables, and table
// bytes and rows per session-minute.
func diskGrowth(rf RunFile) map[string]any {
	ds := diskSamples(rf)
	if len(ds) < 2 {
		return map[string]any{"note": "no disk samples"}
	}
	a, b := ds[0], ds[len(ds)-1]
	ta, _ := num(a, "t")
	tb, _ := num(b, "t")
	hours := (tb - ta) / 3600
	out := map[string]any{"hours": round(hours, 2)}
	per := func(name string, keys ...string) {
		x, ok1 := num(a, keys...)
		y, ok2 := num(b, keys...)
		if ok1 && ok2 && hours > 0 {
			out[name] = round((y-x)/hours, 2)
		}
	}
	per("snapshots_mb_per_hour", "snapshots_mb")
	per("journal_mb_per_hour", "journal_mb")
	per("events_mb_per_hour", "tables", "events", "mb")
	per("events_rows_per_hour", "tables", "events", "rows")
	per("samples_rows_per_hour", "tables", "samples", "rows")
	per("sessions_rows_per_hour", "tables", "sessions", "rows")
	// Session-minutes: labs running integrated over the samples.
	var sm float64
	for i := 1; i < len(ds); i++ {
		t0, _ := num(ds[i-1], "t")
		t1, _ := num(ds[i], "t")
		n, _ := num(ds[i-1], "active")
		sm += n * (t1 - t0) / 60
	}
	out["session_minutes"] = round(sm, 0)
	if sm > 0 {
		ev0, _ := num(a, "tables", "events", "mb")
		ev1, _ := num(b, "tables", "events", "mb")
		r0, _ := num(a, "tables", "events", "rows")
		r1, _ := num(b, "tables", "events", "rows")
		out["events_kb_per_session_minute"] = round((ev1-ev0)*1024/sm, 2)
		out["events_rows_per_session_minute"] = round((r1-r0)/sm, 2)
	}
	return out
}

// imageSizes takes labbase and perf image sizes from any disk sample.
func imageSizes(runs map[string]LoadedRun) map[string]any {
	for _, k := range sortedKeys(runs) {
		for _, d := range diskSamples(runs[k].Run) {
			base, ok1 := num(d, "images_mib", "labbase")
			p, ok2 := num(d, "images_mib", "perf")
			if ok1 && ok2 {
				layer := math.Max(p-base, 0)
				return map[string]any{"labbase": base, "perf": p, "program_layer": round(layer, 2),
					"challenges_200_estimate_basis": round(base+200*layer, 0), "file": runs[k].File}
			}
		}
	}
	return nil
}
