// Package metrics is labd's resource sampler (spec "Metrics & analytics"; Phase 7, task 7.1):
// host memory, CPU and pressure, each lab's cgroup and Sentry, terminal bytes, written to the
// samples table every metrics_flush_s. The readers in this file are shared with labd-perf's
// collector (internal/perf). Paths are relative to a root ("/" in production, testdata in
// tests).
package metrics

import (
	"bufio"
	"bytes"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ProcKV reads "Key:   123 kB" lines (/proc/meminfo, /proc/<pid>/status) into kB values.
func ProcKV(path string) map[string]int64 {
	out := map[string]int64{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fs := strings.Fields(v)
		if len(fs) == 0 {
			continue
		}
		n, err := strconv.ParseInt(fs[0], 10, 64)
		if err == nil {
			out[strings.TrimSpace(k)] = n
		}
	}
	return out
}

// SpaceKV reads "key value" lines (cpu.stat, /proc/vmstat).
func SpaceKV(path string) map[string]int64 {
	out := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		fs := strings.Fields(l)
		if len(fs) != 2 {
			continue
		}
		if n, err := strconv.ParseInt(fs[1], 10, 64); err == nil {
			out[fs[0]] = n
		}
	}
	return out
}

// CPUTimes is the aggregate "cpu" line of /proc/stat in jiffies.
type CPUTimes struct{ Total, Idle uint64 }

// ReadCPU reads /proc/stat's "cpu" line: total and idle (idle + iowait).
func ReadCPU(path string) CPUTimes {
	f, err := os.Open(path)
	if err != nil {
		return CPUTimes{}
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) < 6 || fs[0] != "cpu" {
			continue
		}
		var t CPUTimes
		for i, v := range fs[1:] {
			n, _ := strconv.ParseUint(v, 10, 64)
			if i == 8 || i == 9 { // guest and guest_nice are already counted in user and nice
				continue
			}
			t.Total += n
			if i == 3 || i == 4 {
				t.Idle += n
			}
		}
		return t
	}
	return CPUTimes{}
}

// CPUPct is the busy share of all CPUs between two readings, in % (0 if unknown).
func CPUPct(prev, cur CPUTimes) float64 {
	if prev.Total == 0 || cur.Total <= prev.Total {
		return 0
	}
	dt := float64(cur.Total - prev.Total)
	return Round(100*(dt-float64(cur.Idle-prev.Idle))/dt, 2)
}

// ReadPSI returns "some avg10" from a /proc/pressure file (0 if absent).
func ReadPSI(path string) float64 {
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

// LabCgroup is one lab's cgroup: memory.current in MiB and cpu.stat usage_usec.
type LabCgroup struct {
	MemMB   float64
	CPUUsec uint64
}

// ReadLabCgroups reads every lab-<session> cgroup under dir, keyed by session id.
func ReadLabCgroups(dir string) map[string]LabCgroup {
	out := map[string]LabCgroup{}
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
		out[id] = LabCgroup{MemMB: Round(mem/(1<<20), 2), CPUUsec: uint64(SpaceKV(filepath.Join(d, "cpu.stat"))["usage_usec"])}
	}
	return out
}

// labID finds the session id in a gVisor process's arguments ("lab-<uuid>").
var labID = regexp.MustCompile(`lab-([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// ProcRSS is resident memory by process kind, in MiB.
type ProcRSS struct {
	Sentry, Gofer, Shim map[string]float64 // by session id
	Containerd          float64
}

// ScanProcs reads cmdline and VmRSS of every process under procDir. What counts:
// runsc-sandbox (the Sentry), runsc-gofer, containerd-shim-runsc-v1 (one per lab, outside the
// lab's cgroup) and containerd itself.
func ScanProcs(procDir string) ProcRSS {
	out := ProcRSS{Sentry: map[string]float64{}, Gofer: map[string]float64{}, Shim: map[string]float64{}}
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
		var kind map[string]float64
		switch filepath.Base(string(args[0])) {
		case "runsc-sandbox":
			kind = out.Sentry
		case "runsc-gofer":
			kind = out.Gofer
		case "containerd-shim-runsc-v1":
			kind = out.Shim
		case "containerd":
			out.Containerd += RSSMB(d)
			continue
		default:
			continue
		}
		if m := labID.FindSubmatch(cmd); m != nil {
			kind[string(m[1])] += RSSMB(d)
		}
	}
	return out
}

// RSSMB is a process's VmRSS in MiB.
func RSSMB(procPidDir string) float64 {
	return Round(float64(ProcKV(filepath.Join(procPidDir, "status"))["VmRSS"])/1024, 1)
}

// Round rounds x to d decimals.
func Round(x float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(x*p) / p
}
