package metrics

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"gdblabs/labd/internal/store"
)

// Metric names (spec "Sample schema", plus three Phase 4 showed matter).
const (
	HostMemUsedMB   = "host.mem_used_mb"
	HostCPUPct      = "host.cpu_pct" // % of all CPUs since the previous tick
	HostDiskUsedGB  = "host.disk_used_gb"
	HostCPUPressure = "host.cpu_pressure" // /proc/pressure/cpu some avg10
	HostMemPressure = "host.mem_pressure" // /proc/pressure/memory some avg10
	LabdActive      = "labd.active"
	LabdQueued      = "labd.queued"
	SessionRSSMB    = "session.rss_mb" // the lab's cgroup memory.current, MiB
	SessionSentryMB = "session.sentry_rss_mb"
	SessionCPUMS    = "session.cpu_ms" // CPU used since the previous tick, ms
	WSBytesIn       = "ws.bytes_in"    // keystrokes since the previous tick
	WSBytesOut      = "ws.bytes_out"   // terminal output since the previous tick
)

// Source is what the sampler asks the session manager.
type Source interface {
	Running() []string            // session ids of running labs
	Counts() (active, queued int) // active holds a slot or is being created or ended
}

// Sink stores samples (store.Store).
type Sink interface {
	InsertSamples(ctx context.Context, ss ...store.Sample) error
}

// Sampler takes one reading of the host and of every running lab per tick.
type Sampler struct {
	Root      string // "/" in production
	CgroupDir string // the labs' parent cgroup, relative to Root: sys/fs/cgroup/labs
	DiskPath  string // containerd's root, absolute; "" skips host.disk_used_gb
	Src       Source
	WSBytes   func() map[string][2]int64 // cumulative terminal bytes in and out per session
	Sink      Sink
	OnRSS     func(id string, mb float64) // each lab's memory, for sessions.peak_rss_mb
	Log       *slog.Logger

	prevCPU CPUTimes
	prevLab map[string]uint64   // cpu usage_usec
	prevWS  map[string][2]int64 // cumulative bytes
}

func (s *Sampler) path(p string) string { return filepath.Join(s.Root, p) }

// Tick reads everything once and returns the samples, stamped now. CPU and byte metrics are
// deltas: a lab's first tick has no session.cpu_ms.
func (s *Sampler) Tick(now time.Time) []store.Sample {
	var out []store.Sample
	add := func(id, metric string, v float64) {
		out = append(out, store.Sample{TS: now, SessionID: id, Metric: metric, Value: v})
	}
	mi := ProcKV(s.path("proc/meminfo"))
	add("", HostMemUsedMB, Round(float64(mi["MemTotal"]-mi["MemAvailable"])/1024, 1))
	cpu := ReadCPU(s.path("proc/stat"))
	if s.prevCPU.Total > 0 {
		add("", HostCPUPct, CPUPct(s.prevCPU, cpu))
	}
	s.prevCPU = cpu
	add("", HostCPUPressure, ReadPSI(s.path("proc/pressure/cpu")))
	add("", HostMemPressure, ReadPSI(s.path("proc/pressure/memory")))
	if s.DiskPath != "" {
		if gb, ok := diskUsedGB(s.DiskPath); ok {
			add("", HostDiskUsedGB, gb)
		}
	}
	if s.Src != nil {
		active, queued := s.Src.Counts()
		add("", LabdActive, float64(active))
		add("", LabdQueued, float64(queued))
	}

	var running []string
	if s.Src != nil {
		running = s.Src.Running()
	}
	lab := map[string]uint64{}
	if len(running) > 0 {
		cgs := ReadLabCgroups(s.path(s.CgroupDir))
		procs := ScanProcs(s.path("proc"))
		for _, id := range running {
			cg, ok := cgs[id]
			if !ok {
				continue // created a moment ago, or already gone
			}
			add(id, SessionRSSMB, cg.MemMB)
			if s.OnRSS != nil {
				s.OnRSS(id, cg.MemMB)
			}
			if mb := procs.Sentry[id]; mb > 0 {
				add(id, SessionSentryMB, mb)
			}
			if prev, ok := s.prevLab[id]; ok && cg.CPUUsec >= prev {
				add(id, SessionCPUMS, Round(float64(cg.CPUUsec-prev)/1000, 1))
			}
			lab[id] = cg.CPUUsec
		}
	}
	s.prevLab = lab

	if s.WSBytes != nil {
		ws := s.WSBytes()
		for _, id := range running {
			cur, ok := ws[id]
			if !ok {
				continue
			}
			prev := s.prevWS[id]
			add(id, WSBytesIn, float64(max(cur[0]-prev[0], 0)))
			add(id, WSBytesOut, float64(max(cur[1]-prev[1], 0)))
		}
		s.prevWS = ws
	}
	return out
}

// Run ticks every interval until ctx ends and writes each tick in one batch. A failed write
// is logged and dropped: samples are gauges, the next tick has fresh ones.
func (s *Sampler) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			ss := s.Tick(now.UTC())
			wctx, cancel := context.WithTimeout(ctx, every)
			if err := s.Sink.InsertSamples(wctx, ss...); err != nil && s.Log != nil {
				s.Log.Warn("write samples", "n", len(ss), "err", err)
			}
			cancel()
		}
	}
}
