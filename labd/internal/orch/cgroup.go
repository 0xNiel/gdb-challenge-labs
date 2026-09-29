package orch

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CgroupStats is one reading of a lab's cgroup v2 directory (/sys/fs/cgroup/labs/<id>).
// Under gVisor it covers the whole sandbox on the host: Sentry, gofer and the lab's processes.
type CgroupStats struct {
	MemoryCurrentBytes int64 `json:"memory_current_bytes"`
	MemoryPeakBytes    int64 `json:"memory_peak_bytes"` // memory.peak; 0 if the kernel lacks it
	PidsCurrent        int64 `json:"pids_current"`
	PidsPeak           int64 `json:"pids_peak"` // pids.peak; 0 if the kernel lacks it
	CPUUsageUsec       int64 `json:"cpu_usage_usec"`
	NrThrottled        int64 `json:"nr_throttled"`
	ThrottledUsec      int64 `json:"throttled_usec"`
}

// ReadCgroupStats reads the files it knows; missing files leave zero values. It returns an
// error only when the directory itself is gone.
func ReadCgroupStats(dir string) (CgroupStats, error) {
	var s CgroupStats
	if _, err := os.Stat(dir); err != nil {
		return s, err
	}
	s.MemoryCurrentBytes = readInt(filepath.Join(dir, "memory.current"))
	s.MemoryPeakBytes = readInt(filepath.Join(dir, "memory.peak"))
	s.PidsCurrent = readInt(filepath.Join(dir, "pids.current"))
	s.PidsPeak = readInt(filepath.Join(dir, "pids.peak"))
	kv := readKV(filepath.Join(dir, "cpu.stat"))
	s.CPUUsageUsec, s.NrThrottled, s.ThrottledUsec = kv["usage_usec"], kv["nr_throttled"], kv["throttled_usec"]
	return s, nil
}

// Merge keeps the maximum of the gauges and the latest counters. Used while sampling a
// container whose peak files may be missing on older kernels.
func (s *CgroupStats) Merge(n CgroupStats) {
	s.MemoryCurrentBytes = n.MemoryCurrentBytes
	s.PidsCurrent = n.PidsCurrent
	s.MemoryPeakBytes = max(s.MemoryPeakBytes, n.MemoryPeakBytes, n.MemoryCurrentBytes)
	s.PidsPeak = max(s.PidsPeak, n.PidsPeak, n.PidsCurrent)
	s.CPUUsageUsec = max(s.CPUUsageUsec, n.CPUUsageUsec)
	s.NrThrottled = max(s.NrThrottled, n.NrThrottled)
	s.ThrottledUsec = max(s.ThrottledUsec, n.ThrottledUsec)
}

func readInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	v, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0 // e.g. "max"
	}
	return v
}

func readKV(path string) map[string]int64 {
	out := map[string]int64{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			out[k] = n
		}
	}
	return out
}
