package orch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCgroup(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadCgroupStats(t *testing.T) {
	dir := writeCgroup(t, map[string]string{
		"memory.current": "12582912\n",
		"memory.peak":    "15728640\n",
		"pids.current":   "21\n",
		"pids.peak":      "24\n",
		"cpu.stat":       "usage_usec 580284\nuser_usec 400000\nsystem_usec 180284\nnr_periods 12\nnr_throttled 9\nthrottled_usec 420000\n",
	})
	s, err := ReadCgroupStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := CgroupStats{MemoryCurrentBytes: 12582912, MemoryPeakBytes: 15728640, PidsCurrent: 21, PidsPeak: 24,
		CPUUsageUsec: 580284, NrThrottled: 9, ThrottledUsec: 420000}
	if s != want {
		t.Fatalf("got %+v\nwant %+v", s, want)
	}
}

func TestReadCgroupStats_MissingFilesAndMax(t *testing.T) {
	dir := writeCgroup(t, map[string]string{"memory.current": "100\n", "pids.current": "max\n"})
	s, err := ReadCgroupStats(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.MemoryCurrentBytes != 100 || s.MemoryPeakBytes != 0 || s.PidsCurrent != 0 || s.CPUUsageUsec != 0 {
		t.Fatalf("unexpected %+v", s)
	}
	if _, err := ReadCgroupStats(filepath.Join(dir, "gone")); err == nil {
		t.Fatal("missing directory must be an error")
	}
}

func TestMerge_KeepsPeaksWithoutPeakFiles(t *testing.T) {
	var acc CgroupStats
	acc.Merge(CgroupStats{MemoryCurrentBytes: 10, PidsCurrent: 5, CPUUsageUsec: 100})
	acc.Merge(CgroupStats{MemoryCurrentBytes: 30, PidsCurrent: 9, CPUUsageUsec: 300})
	acc.Merge(CgroupStats{MemoryCurrentBytes: 20, PidsCurrent: 7, CPUUsageUsec: 350})
	if acc.MemoryPeakBytes != 30 || acc.PidsPeak != 9 || acc.CPUUsageUsec != 350 || acc.MemoryCurrentBytes != 20 {
		t.Fatalf("got %+v", acc)
	}
}
