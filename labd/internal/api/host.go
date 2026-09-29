package api

import (
	"bufio"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"

	"gdblabs/labd/internal/orch"
)

// readHost reports host memory and load from /proc (zeros where /proc is absent, e.g. macOS
// unit tests). CPU percentages need two samples and come with the Phase 7 sampler.
func readHost() map[string]any {
	mi := procKV("/proc/meminfo") // kB
	total, avail := mi["MemTotal"]/1024, mi["MemAvailable"]/1024
	var load1 float64
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		if f := strings.Fields(string(b)); len(f) > 0 {
			load1, _ = strconv.ParseFloat(f[0], 64)
		}
	}
	return map[string]any{
		"mem_total_mb": total, "mem_available_mb": avail, "mem_used_mb": total - avail,
		"load1": load1, "cpus": runtime.NumCPU(),
	}
}

// selfRSSMiB is labd's own resident memory (VmRSS), in MiB.
func selfRSSMiB() float64 {
	return round1(float64(procKV("/proc/self/status")["VmRSS"]) / 1024)
}

// cgroupRSSMiB is a lab's memory.current in MiB (the whole sandbox under gVisor).
func cgroupRSSMiB(dir string) float64 {
	st, err := orch.ReadCgroupStats(dir)
	if err != nil {
		return 0
	}
	return round1(float64(st.MemoryCurrentBytes) / (1 << 20))
}

// procKV parses "Key:   123 kB" lines.
func procKV(path string) map[string]int64 {
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
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		if n, err := strconv.ParseInt(fields[0], 10, 64); err == nil {
			out[k] = n
		}
	}
	return out
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
