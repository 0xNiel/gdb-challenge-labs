package perf

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

// file assembles the run file: meta, summary and the spec's pass criteria.
func (r *runner) file(ctx context.Context) RunFile {
	r.mu.Lock()
	defer r.mu.Unlock()
	arch, kernel, runsc := hostInfo()
	total := float64(procKV(r.col.path("proc/meminfo"))["MemTotal"]) / 1024
	maxS := 0
	if st, err := r.stats(ctx); err == nil {
		maxS = st.MaxSessions
	}
	rf := RunFile{
		Meta: RunMeta{
			Scenario: r.cfg.Scenario, Host: r.cfg.Host, Arch: arch, Kernel: kernel, Runtime: r.cfg.Runtime,
			Platform: r.cfg.Platform, Runsc: runsc, Label: r.cfg.Label, Date: r.t0.UTC().Format(time.RFC3339),
			N: r.cfg.N, Ramp: r.cfg.Ramp, HoldS: int(r.cfg.Hold.Seconds()), Mix: r.cfg.Mix, MaxSessions: maxS,
			HostMemMB: round(total, 0), CPUs: runtime.NumCPU(), Partial: r.cfg.Partial, Command: r.cfg.Command,
			DurationS: round(time.Since(r.t0).Seconds(), 0),
		},
		Timeline: r.marks, Extra: r.extra, VUsers: r.users, Samples: r.samples,
	}
	if rf.Meta.Runtime != "io.containerd.runsc.v1" {
		rf.Meta.Runsc = ""
	}
	rf.Summary = r.summary()
	rf.Criteria = r.criteria(rf.Summary)
	return rf
}

func (r *runner) holdSamples() []Sample {
	var out []Sample
	for _, s := range r.samples {
		if r.hold[0] > 0 && s.T >= r.hold[0] && (r.hold[1] == 0 || s.T <= r.hold[1]) {
			out = append(out, s)
		}
	}
	return out
}

func (r *runner) summary() RunSummary {
	var s RunSummary
	s.Sessions = len(r.users)
	profileOf := map[string]string{}
	var start, create, queue, echo, step []float64
	byProfile := map[string][]float64{}
	byVerb := map[string][]float64{}
	var bpsIn, bpsOut []float64
	for _, u := range r.users {
		profileOf[u.SessionID] = u.Profile
		if u.Err != "" && u.Ended != "hard_ttl" { // a churn survivor reaching its TTL is correct
			s.VUserErrors++
		}
		s.Commands += u.Commands
		s.CommandErrors += u.Errors
		if u.StartToPromptMS > 0 {
			start = append(start, u.StartToPromptMS)
		}
		if u.CreateHTTPMS > 0 {
			create = append(create, u.CreateHTTPMS)
		}
		if u.QueueWaitMS > 0 {
			queue = append(queue, u.QueueWaitMS)
		}
		echo = append(echo, u.EchoMS...)
		byProfile[u.Profile] = append(byProfile[u.Profile], u.EchoMS...)
		for verb, xs := range u.CmdMS {
			byVerb[verb] = append(byVerb[verb], xs...)
		}
		step = append(step, u.CmdMS["next"]...)
		step = append(step, u.CmdMS["step"]...)
		if u.HoldS >= 30 {
			bpsIn = append(bpsIn, float64(u.BytesIn)/u.HoldS)
			bpsOut = append(bpsOut, float64(u.BytesOut)/u.HoldS)
		}
	}
	s.StartToPromptMS, s.CreateHTTPMS, s.QueueWaitMS = Percentiles(start), Percentiles(create), Percentiles(queue)
	s.EchoMS, s.StepCmdMS = Percentiles(echo), Percentiles(step)
	s.EchoMSByProfile = map[string]Pctl{}
	for p, xs := range byProfile {
		s.EchoMSByProfile[p] = Percentiles(xs)
	}
	s.CmdMS = map[string]Pctl{}
	for v, xs := range byVerb {
		s.CmdMS[v] = Percentiles(xs)
	}
	s.WSBpsIn, s.WSBpsOut = round(Mean(bpsIn), 1), round(Mean(bpsOut), 1)
	var aggOut float64
	for _, x := range bpsOut {
		aggOut += x
	}
	s.WSBpsOutAggregate = round(aggOut, 0)

	hold := r.holdSamples()
	s.HoldSamples = len(hold)
	// The idle host before the run can still be releasing memory (the wrapper builds labd
	// just before); the idle sample after teardown is the other bound. Take the lower, so the
	// per-lab figure errs high.
	s.HostBaselineMB = r.idle.MemUsedMB
	if n := len(r.samples); n > 0 && r.samples[n-1].MemUsedMB > 0 {
		s.HostAfterMB = r.samples[n-1].MemUsedMB
		s.HostBaselineMB = min(s.HostBaselineMB, s.HostAfterMB)
	}
	base := s.HostBaselineMB
	var mem, sentry, host, cpu, used, hcpu []float64
	cpuBy := map[string][]float64{}
	perLab := 0.0
	for _, h := range hold {
		s.HoldLabs = max(s.HoldLabs, h.LabCount)
		used = append(used, h.MemUsedMB)
		hcpu = append(hcpu, h.CPUPct)
		if h.LabCount > 0 && h.MemUsedMB-base > perLab*float64(h.LabCount) {
			perLab = (h.MemUsedMB - base) / float64(h.LabCount)
		}
		for _, l := range h.Labs {
			mem = append(mem, l.MemMB)
			if l.SentryMB > 0 {
				sentry = append(sentry, l.SentryMB)
				host = append(host, l.HostRSSMB)
			}
			if l.CPUPct >= 0 {
				cpu = append(cpu, l.CPUPct)
				cpuBy[profileOf[l.SessionID]] = append(cpuBy[profileOf[l.SessionID]], l.CPUPct)
			}
		}
	}
	s.LabMemMB, s.SentryRSSMB, s.LabHostRSSMB = Percentiles(mem), Percentiles(sentry), Percentiles(host)
	s.LabCPUPctAvg = round(Mean(cpu), 2)
	s.LabCPUPctByProfile = map[string]float64{}
	for p, xs := range cpuBy {
		if p != "" {
			s.LabCPUPctByProfile[p] = round(Mean(xs), 2)
		}
	}
	s.HostMemUsedMB, s.HostCPUPct = p3(used), p3(hcpu)
	s.HostPerLabMB = round(perLab, 1)

	for _, x := range r.samples {
		s.PSIMax.CPU = max(s.PSIMax.CPU, x.PSI.CPU)
		s.PSIMax.Memory = max(s.PSIMax.Memory, x.PSI.Memory)
		s.PSIMax.IO = max(s.PSIMax.IO, x.PSI.IO)
		s.LabdRSSMB.Peak = max(s.LabdRSSMB.Peak, x.Stats.Labd.RSSMB)
		s.LabdGoroutines.Peak = max(s.LabdGoroutines.Peak, float64(x.Stats.Labd.Goroutines))
		s.ContainerdRSSMB.Peak = max(s.ContainerdRSSMB.Peak, x.ContainerdRSSMB)
		s.ContainersMax = max(s.ContainersMax, x.Containers)
	}
	if len(r.samples) > 0 {
		first, last := r.samples[0], r.samples[len(r.samples)-1]
		s.OOMKills = last.OOMKills - first.OOMKills
		s.LabdRSSMB.Idle, s.LabdRSSMB.After = r.idle.Stats.Labd.RSSMB, last.Stats.Labd.RSSMB
		s.LabdGoroutines.Idle, s.LabdGoroutines.After = float64(r.idle.Stats.Labd.Goroutines), float64(last.Stats.Labd.Goroutines)
		s.ContainerdRSSMB.Idle, s.ContainerdRSSMB.After = r.idle.ContainerdRSSMB, last.ContainerdRSSMB
		s.Leaks = Leaks{
			Containers: max(last.Containers, 0),
			Goroutines: max(last.Stats.Labd.Goroutines-r.idle.Stats.Labd.Goroutines, 0),
			FIFOs:      max(last.FIFOs-r.idle.FIFOs, 0),
		}
	}
	return s
}

func (r *runner) criteria(s RunSummary) []Criterion {
	var cs []Criterion
	add := func(pass bool, criterion, format string, a ...any) {
		cs = append(cs, Criterion{Criterion: criterion, Measured: fmt.Sprintf(format, a...), Pass: pass})
	}
	noErrors := func() {
		add(s.VUserErrors == 0, "every simulated user completes", "%d of %d failed", s.VUserErrors, s.Sessions)
	}
	noLeaks := func() {
		l := s.Leaks
		add(l.Containers == 0 && l.Goroutines <= 2 && l.FIFOs == 0, "nothing left after every session ended",
			"containers %d, goroutines +%d, FIFOs +%d", l.Containers, l.Goroutines, l.FIFOs)
	}
	hostCriteria := func() {
		add(s.HostMemUsedMB.Max < 24*1024, "host memory used < 24 GB", "%.0f MB peak (%.0f MB before any lab)", s.HostMemUsedMB.Max, s.HostBaselineMB)
		add(s.OOMKills == 0, "no OOM kills", "%d", s.OOMKills)
		add(s.EchoMS.N > 0 && s.EchoMS.P95 < 100, "keystroke echo p95 < 100 ms", "p95 %.1f ms over %d commands", s.EchoMS.P95, s.EchoMS.N)
	}
	switch r.cfg.Scenario {
	case "P1":
		add(s.StartToPromptMS.N > 0 && s.StartToPromptMS.P95 < 2000, "start latency p95 < 2 s",
			"%.0f ms (n=%d: one start per session)", s.StartToPromptMS.P95, s.StartToPromptMS.N)
		noErrors()
	case "P2", "P8":
		hostCriteria()
		add(s.StartToPromptMS.P95 < 2000, "start latency p95 < 2 s (under the ramp)", "p95 %.0f ms, n=%d", s.StartToPromptMS.P95, s.StartToPromptMS.N)
		noErrors()
		if r.cfg.Scenario == "P8" {
			add(s.WSBpsOut > 0, "bytes/s per session and aggregate recorded", "%.1f B/s out and %.1f B/s in per session, %.0f B/s out in total",
				s.WSBpsOut, s.WSBpsIn, s.WSBpsOutAggregate)
		}
	case "P3":
		hostCriteria()
		var others []float64
		for _, u := range r.users {
			if u.Profile != Abuser {
				others = append(others, u.EchoMS...)
			}
		}
		op := Percentiles(others)
		add(op.P95 < 100, "others' echo p95 < 100 ms beside the abusers", "p95 %.1f ms (compare P2 in the report)", op.P95)
		bounded, detail := r.abusersBounded()
		add(bounded, "abusers capped by the sandbox and gateway limits", "%s", detail)
		noErrors()
	case "P4", "P9":
		over, _ := r.extra["over_cap"].([]string)
		qc, _ := r.extra["quiet_containers"].(int)
		qa, _ := r.extra["quiet_active"].(int)
		add(len(over) == 0, "containers never above the cap", "%d samples over", len(over))
		add(qc == qa, "no container leak: containers == active once quiet", "containers %d, active %d", qc, qa)
		first, last := r.startThirds()
		add(last.N > 0 && last.P95 <= first.P95*1.5+250, "start latency stable", "start to prompt p95 %.0f ms in the first third, %.0f ms in the last", first.P95, last.P95)
		noLeaks()
		noErrors()
	case "P5":
		adopted, _ := r.extra["adopted_ms"].(float64)
		add(adopted >= 0 && adopted < 15000 && r.extra["adopted_ms"] != nil, "all containers reconciled in < 15 s", "%.0f ms from labd answering to every lab running", adopted)
		re := 0
		for _, u := range r.users {
			if u.Reconnects > 0 && u.Err == "" {
				re++
			}
		}
		add(re == r.cfg.N, "reconnecting clients within the grace resume", "%d of %d reconnected to their own lab", re, r.cfg.N)
		noLeaks()
	case "P6":
		p6, _ := r.extra["p6"].(map[string]any)
		get := func(k string) any { return p6[k] }
		capN, _ := get("cap").(int)
		wantQ := min(r.cfg.N-capN, intOr(get("max_queue")))
		add(get("admitted") == capN, "admitted exactly the cap", "%v of cap %d", get("admitted"), capN)
		add(get("queued") == wantQ && get("positions_in_order") == true, "the rest queued with correct positions",
			"%v queued (want %d), positions in order: %v", get("queued"), wantQ, get("positions_in_order"))
		add(get("never_over_cap") == true, "none admitted over the cap", "%v", get("over_cap"))
		add(get("drained_fifo") == true, "the queue drains first in, first out", "%v of %v admitted from the queue in order: %v",
			get("admitted_from_queue"), get("queued"), get("drained_fifo"))
		noLeaks()
	case "P7":
		p7, _ := r.extra["p7"].(map[string]any)
		wo, _ := p7["without_image"].(map[string]any)
		failed := wo != nil && (wo["ended"] == "create_failed" || wo["err"] != "")
		add(failed, "a start without the image fails: pre-pull is required", "%v", wo)
		ap, _ := p7["after_pull_start_to_prompt_ms"].(Pctl)
		w, _ := p7["warm_start_to_prompt_ms"].(Pctl)
		add(ap.N > 0 && w.N > 0, "time to first prompt with pull vs pre-pulled recorded",
			"pull %.0f ms; then start to prompt p50 %.0f ms (cold cache), %.0f ms warm", floatOr(p7["pull_ms"]), ap.P50, w.P50)
		noLeaks()
	}
	return cs
}

func intOr(v any) int {
	if n, ok := v.(int); ok {
		return n
	}
	return 1 << 30
}

func floatOr(v any) float64 {
	f, _ := v.(float64)
	return f
}

// startThirds splits start-to-prompt latencies by start order into thirds (churn stability).
func (r *runner) startThirds() (first, last Pctl) {
	us := slices.Clone(r.users)
	slices.SortFunc(us, func(a, b VUserResult) int { return int(a.UserID - b.UserID) })
	var xs []float64
	for _, u := range us {
		if u.StartToPromptMS > 0 {
			xs = append(xs, u.StartToPromptMS)
		}
	}
	n := len(xs) / 3
	if n == 0 {
		return Pctl{}, Pctl{}
	}
	return Percentiles(xs[:n]), Percentiles(xs[len(xs)-n:])
}

var (
	forkRe = regexp.MustCompile(`forked=(\d+) error=(\w+)`)
	fillRe = regexp.MustCompile(`bytes=(\d+) error=(\w+)`)
)

// abusersBounded checks each abuser: fork stopped by the process limit (S7: 32), the write
// stopped by the 16 MB tmpfs or the 32 MB file limit (S4, S7), the paste cut by the input
// limit with a warn (S13), and the CPU loop held to the lab's quota.
func (r *runner) abusersBounded() (bool, string) {
	ok, n := true, 0
	var bad []string
	cpuCap := r.labCPUMax(Abuser)
	for _, u := range r.users {
		if u.Profile != Abuser {
			continue
		}
		n++
		for _, a := range u.Abuse {
			switch a.Action {
			case "fork":
				m := forkRe.FindStringSubmatch(a.Output)
				if m == nil || m[2] == "OK" || atoi(m[1]) > 40 {
					ok, bad = false, append(bad, fmt.Sprintf("user %d fork %q", u.UserID, a.Output))
				}
			case "write_20mb":
				m := fillRe.FindStringSubmatch(a.Output)
				if m == nil || m[2] == "OK" || atoi(m[1]) > 32<<20 {
					ok, bad = false, append(bad, fmt.Sprintf("user %d write %q", u.UserID, a.Output))
				}
			case "paste_100kb":
				if !a.Warned {
					ok, bad = false, append(bad, fmt.Sprintf("user %d paste without a warn", u.UserID))
				}
			}
		}
		if len(u.Abuse) < 4 {
			ok, bad = false, append(bad, fmt.Sprintf("user %d ran %d of 4 actions (%s)", u.UserID, len(u.Abuse), u.Err))
		}
	}
	if n == 0 {
		return false, "no abuser ran"
	}
	quota := 50.0 // default_limits cpu_millicores 500 = half a core
	if cpuCap > quota*1.15 {
		ok, bad = false, append(bad, fmt.Sprintf("an abuser lab used %.0f %% of a core, over its %.0f %% quota", cpuCap, quota))
	}
	if ok {
		return true, fmt.Sprintf("%d abusers: fork, 20 MB write and paste all stopped; busiest abuser lab %.0f %% of a core (quota %.0f %%)", n, cpuCap, quota)
	}
	return false, strings.Join(bad, "; ")
}

// labCPUMax is the highest per-sample CPU of any lab running the profile.
func (r *runner) labCPUMax(profile string) float64 {
	ids := map[string]bool{}
	for _, u := range r.users {
		if u.Profile == profile {
			ids[u.SessionID] = true
		}
	}
	m := 0.0
	for _, s := range r.holdSamples() {
		for _, l := range s.Labs {
			if ids[l.SessionID] {
				m = max(m, l.CPUPct)
			}
		}
	}
	return m
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }
