package perf

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"gdblabs/labd/internal/metrics"
)

// Collector samples the host while a scenario runs (plan "Collector"): host memory, CPU and
// pressure, every lab's cgroup, the gVisor processes of each lab, containerd, and labd's own
// view through GET /internal/stats. Everything is read under Root, so tests point it at a
// fake /proc and cgroup tree.
type Collector struct {
	Root      string // "/" on the lab host
	CgroupDir string // the labs' parent cgroup, relative to Root (sys/fs/cgroup/labs)
	FIFODir   string // labd's FIFO directory (absolute); "" skips the count
	// Stats is GET /internal/stats; Containers counts containers in namespace labs. Either
	// may be nil (tests).
	Stats      func(ctx context.Context) (LabdStats, error)
	Containers func(ctx context.Context) (int, error)

	mu      sync.Mutex // Sample keeps deltas between calls; the loop and teardown both call it
	start   time.Time
	prevCPU metrics.CPUTimes
	prevLab map[string]labCPU
	prevAt  time.Time
}

// LabdStats is the part of GET /internal/stats the collector keeps.
type LabdStats struct {
	Active      int `json:"active"`
	Creating    int `json:"creating"`
	Running     int `json:"running"`
	Ending      int `json:"ending"`
	Queued      int `json:"queued"`
	MaxSessions int `json:"max_sessions"`
	MaxQueue    int `json:"max_queue"`
	Labd        struct {
		Goroutines int     `json:"goroutines"`
		RSSMB      float64 `json:"rss_mb"`
	} `json:"labd"`
}

// Sample is one tick. Per-lab values are summarised here; the raw values are in Labs for
// the scenario to pool, and are not written.
type Sample struct {
	T               float64   `json:"t"` // seconds since the collector started
	MemUsedMB       float64   `json:"mem_used_mb"`
	MemAvailMB      float64   `json:"mem_available_mb"`
	CPUPct          float64   `json:"cpu_pct"` // of all host CPUs, 0-100
	PSI             PSI       `json:"psi"`
	OOMKills        int64     `json:"oom_kills"` // /proc/vmstat oom_kill, cumulative since boot
	Stats           LabdStats `json:"labd_stats"`
	StatsErr        string    `json:"stats_err,omitempty"`
	Containers      int       `json:"containers"` // -1: not counted this tick
	ContainerdRSSMB float64   `json:"containerd_rss_mb"`
	FIFOs           int       `json:"fifos"`
	LabCount        int       `json:"labs"`
	LabMemMB        P50P95Max `json:"lab_mem_mb"` // cgroup memory.current
	LabMemSumMB     float64   `json:"lab_mem_sum_mb"`
	SentryRSSMB     P50P95Max `json:"sentry_rss_mb"` // the runsc-sandbox process
	LabHostRSSMB    P50P95Max `json:"lab_host_rss_mb"`
	LabCPUPct       P50P95Max `json:"lab_cpu_pct"` // per lab, % of one core
	LabCPUPctSum    float64   `json:"lab_cpu_pct_sum"`

	Labs []LabSample `json:"-"`
}

// PSI is "some avg10" from /proc/pressure.
type PSI struct {
	CPU    float64 `json:"cpu_some_avg10"`
	Memory float64 `json:"memory_some_avg10"`
	IO     float64 `json:"io_some_avg10"`
}

// LabSample is one lab at one tick.
type LabSample struct {
	SessionID string
	MemMB     float64 // cgroup memory.current (the whole sandbox)
	CPUPct    float64 // % of one core since the previous tick (-1 on the first)
	SentryMB  float64 // runsc-sandbox RSS
	HostRSSMB float64 // sandbox + gofer + shim RSS (RSS counts shared pages)
}

type labCPU struct{ usec uint64 }

func (c *Collector) path(p string) string { return filepath.Join(c.Root, p) }

// Sample takes one tick at now. Containers are counted only when count is true (the plan
// counts them every 30 s; it asks containerd).
func (c *Collector) Sample(ctx context.Context, now time.Time, count bool) Sample {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.start.IsZero() {
		c.start = now
	}
	s := Sample{T: round(now.Sub(c.start).Seconds(), 1), Containers: -1}
	mi := procKV(c.path("proc/meminfo"))
	s.MemAvailMB = round(float64(mi["MemAvailable"])/1024, 1)
	s.MemUsedMB = round(float64(mi["MemTotal"]-mi["MemAvailable"])/1024, 1)
	cpu := metrics.ReadCPU(c.path("proc/stat"))
	s.CPUPct = metrics.CPUPct(c.prevCPU, cpu)
	c.prevCPU = cpu
	s.PSI = PSI{CPU: readPSI(c.path("proc/pressure/cpu")), Memory: readPSI(c.path("proc/pressure/memory")), IO: readPSI(c.path("proc/pressure/io"))}
	s.OOMKills = spaceKV(c.path("proc/vmstat"))["oom_kill"]

	if c.Stats != nil {
		if st, err := c.Stats(ctx); err != nil {
			s.StatsErr = err.Error()
		} else {
			s.Stats = st
		}
	}
	if count && c.Containers != nil {
		if n, err := c.Containers(ctx); err == nil {
			s.Containers = n
		}
	}
	if c.FIFODir != "" {
		if es, err := os.ReadDir(c.FIFODir); err == nil {
			s.FIFOs = len(es)
		}
	}

	procs := scanProcs(c.path("proc"))
	s.ContainerdRSSMB = procs.Containerd
	cgs := readLabCgroups(c.path(c.CgroupDir))
	wall := now.Sub(c.prevAt)
	lab := map[string]labCPU{}
	var mem, sentry, host, cpus []float64
	for id, cg := range cgs {
		ls := LabSample{SessionID: id, MemMB: cg.MemMB, CPUPct: -1, SentryMB: procs.Sentry[id],
			HostRSSMB: round(procs.Sentry[id]+procs.Gofer[id]+procs.Shim[id], 1)}
		if p, ok := c.prevLab[id]; ok && wall > 0 && cg.CPUUsec >= p.usec {
			ls.CPUPct = round(100*float64(cg.CPUUsec-p.usec)/float64(wall.Microseconds()), 2)
			cpus = append(cpus, ls.CPUPct)
			s.LabCPUPctSum += ls.CPUPct
		}
		lab[id] = labCPU{usec: cg.CPUUsec}
		mem = append(mem, ls.MemMB)
		s.LabMemSumMB += ls.MemMB
		if ls.SentryMB > 0 {
			sentry = append(sentry, ls.SentryMB)
			host = append(host, ls.HostRSSMB)
		}
		s.Labs = append(s.Labs, ls)
	}
	c.prevLab, c.prevAt = lab, now
	s.LabCount = len(cgs)
	s.LabMemMB, s.SentryRSSMB, s.LabHostRSSMB, s.LabCPUPct = p3(mem), p3(sentry), p3(host), p3(cpus)
	s.LabMemSumMB, s.LabCPUPctSum = round(s.LabMemSumMB, 1), round(s.LabCPUPctSum, 2)
	return s
}

func p3(xs []float64) P50P95Max {
	p := Percentiles(xs)
	return P50P95Max{P50: round(p.P50, 2), P95: round(p.P95, 2), Max: round(p.Max, 2)}
}

// The /proc and cgroup readers live in internal/metrics, which labd's sampler shares.
func procKV(path string) map[string]int64                    { return metrics.ProcKV(path) }
func spaceKV(path string) map[string]int64                   { return metrics.SpaceKV(path) }
func readPSI(path string) float64                            { return metrics.ReadPSI(path) }
func readLabCgroups(dir string) map[string]metrics.LabCgroup { return metrics.ReadLabCgroups(dir) }
func scanProcs(procDir string) metrics.ProcRSS               { return metrics.ScanProcs(procDir) }
