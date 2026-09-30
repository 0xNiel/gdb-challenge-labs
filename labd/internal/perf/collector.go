package perf

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
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

	start   time.Time
	prevCPU cpuTimes
	prevLab map[string]labCPU
	prevAt  time.Time
}

// LabdStats is the part of GET /internal/stats the collector keeps.
type LabdStats struct {
	Active   int `json:"active"`
	Creating int `json:"creating"`
	Running  int `json:"running"`
	Ending   int `json:"ending"`
	Queued   int `json:"queued"`
	Labd     struct {
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

type cpuTimes struct{ total, idle uint64 }

type labCPU struct{ usec uint64 }

func (c *Collector) path(p string) string { return filepath.Join(c.Root, p) }

// Sample takes one tick at now. Containers are counted only when count is true (the plan
// counts them every 30 s; it asks containerd).
func (c *Collector) Sample(ctx context.Context, now time.Time, count bool) Sample {
	if c.start.IsZero() {
		c.start = now
	}
	s := Sample{T: round(now.Sub(c.start).Seconds(), 1), Containers: -1}
	mi := procKV(c.path("proc/meminfo"))
	s.MemAvailMB = round(float64(mi["MemAvailable"])/1024, 1)
	s.MemUsedMB = round(float64(mi["MemTotal"]-mi["MemAvailable"])/1024, 1)
	cpu := readCPU(c.path("proc/stat"))
	if c.prevCPU.total > 0 && cpu.total > c.prevCPU.total {
		dt := float64(cpu.total - c.prevCPU.total)
		s.CPUPct = round(100*(dt-float64(cpu.idle-c.prevCPU.idle))/dt, 2)
	}
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
	s.ContainerdRSSMB = procs.containerd
	cgs := readLabCgroups(c.path(c.CgroupDir))
	wall := now.Sub(c.prevAt)
	lab := map[string]labCPU{}
	var mem, sentry, host, cpus []float64
	for id, cg := range cgs {
		ls := LabSample{SessionID: id, MemMB: cg.memMB, CPUPct: -1, SentryMB: procs.sentry[id],
			HostRSSMB: round(procs.sentry[id]+procs.gofer[id]+procs.shim[id], 1)}
		if p, ok := c.prevLab[id]; ok && wall > 0 && cg.cpuUsec >= p.usec {
			ls.CPUPct = round(100*float64(cg.cpuUsec-p.usec)/float64(wall.Microseconds()), 2)
			cpus = append(cpus, ls.CPUPct)
			s.LabCPUPctSum += ls.CPUPct
		}
		lab[id] = labCPU{usec: cg.cpuUsec}
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

// procKV parses "Key:   123 kB" lines (meminfo, status), values as written (kB).
func procKV(path string) map[string]int64 {
	out := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		if f := strings.Fields(v); len(f) > 0 {
			n, _ := strconv.ParseInt(f[0], 10, 64)
			out[strings.TrimSpace(k)] = n
		}
	}
	return out
}

// spaceKV parses "key value" lines (vmstat, cpu.stat).
func spaceKV(path string) map[string]int64 {
	out := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		if f := strings.Fields(l); len(f) == 2 {
			n, _ := strconv.ParseInt(f[1], 10, 64)
			out[f[0]] = n
		}
	}
	return out
}

// readCPU is the aggregate "cpu" line of /proc/stat: total and idle (idle + iowait) jiffies.
func readCPU(path string) cpuTimes {
	f, err := os.Open(path)
	if err != nil {
		return cpuTimes{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 6 || fs[0] != "cpu" {
			continue
		}
		var t cpuTimes
		for i, v := range fs[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i == 8 || i == 9 { // guest and guest_nice are already counted in user and nice
				continue
			}
			t.total += n
			if i == 3 || i == 4 {
				t.idle += n
			}
		}
		return t
	}
	return cpuTimes{}
}

// readPSI returns "some avg10" from a /proc/pressure file (0 if absent).
func readPSI(path string) float64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(l, "some ") {
			continue
		}
		for _, f := range strings.Fields(l) {
			if v, ok := strings.CutPrefix(f, "avg10="); ok {
				x, _ := strconv.ParseFloat(v, 64)
				return x
			}
		}
	}
	return 0
}

type labCgroup struct {
	memMB   float64
	cpuUsec uint64
}

// readLabCgroups reads every lab-<session> cgroup under dir.
func readLabCgroups(dir string) map[string]labCgroup {
	out := map[string]labCgroup{}
	es, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range es {
		id, ok := strings.CutPrefix(e.Name(), "lab-")
		if !ok || !e.IsDir() {
			continue
		}
		d := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(filepath.Join(d, "memory.current"))
		if err != nil {
			continue // the lab went away between ReadDir and here
		}
		mem, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
		out[id] = labCgroup{memMB: round(mem/(1<<20), 2), cpuUsec: uint64(spaceKV(filepath.Join(d, "cpu.stat"))["usage_usec"])}
	}
	return out
}

// labID finds the session id in a gVisor process's arguments ("lab-<uuid>").
var labID = regexp.MustCompile(`lab-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

type procRSS struct {
	sentry, gofer, shim map[string]float64 // by session id, MiB
	containerd          float64
}

// scanProcs reads comm, cmdline and VmRSS of every process under procDir. What counts:
// runsc-sandbox (the Sentry; comm gvisor_sentry), runsc-gofer, containerd-shim-runsc-v1
// (one per lab, outside the lab's cgroup) and containerd itself.
func scanProcs(procDir string) procRSS {
	out := procRSS{sentry: map[string]float64{}, gofer: map[string]float64{}, shim: map[string]float64{}}
	es, err := os.ReadDir(procDir)
	if err != nil {
		return out
	}
	for _, e := range es {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		d := filepath.Join(procDir, e.Name())
		cmd, err := os.ReadFile(filepath.Join(d, "cmdline"))
		if err != nil || len(cmd) == 0 {
			continue
		}
		args := bytes.Split(bytes.TrimRight(cmd, "\x00"), []byte{0})
		arg0 := filepath.Base(string(args[0]))
		var kind map[string]float64
		switch arg0 {
		case "runsc-sandbox":
			kind = out.sentry
		case "runsc-gofer":
			kind = out.gofer
		case "containerd-shim-runsc-v1":
			kind = out.shim
		case "containerd":
			out.containerd += rssMB(d)
			continue
		default:
			continue
		}
		if m := labID.FindSubmatch(cmd); m != nil {
			kind[string(m[1])] += rssMB(d)
		}
	}
	return out
}

func rssMB(procPidDir string) float64 {
	return round(float64(procKV(filepath.Join(procPidDir, "status"))["VmRSS"])/1024, 1)
}
