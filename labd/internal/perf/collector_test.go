package perf

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	idA = "11111111-2222-3333-4444-555555555555"
	idB = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// fakeHost writes a /proc and cgroup tree like the lab host's, with two labs.
func fakeHost(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	put := func(p, s string) {
		t.Helper()
		p = filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("proc/meminfo", "MemTotal:       16000000 kB\nMemFree:  1000 kB\nMemAvailable:   12000000 kB\n")
	put("proc/stat", "cpu  1000 0 500 8000 500 0 0 0 0 0\ncpu0 1 2 3 4 5 6 7 8 9 10\n")
	put("proc/pressure/memory", "some avg10=1.50 avg60=0.20 avg300=0.00 total=10\nfull avg10=0.10 avg60=0.00 avg300=0.00 total=1\n")
	put("proc/pressure/cpu", "some avg10=12.25 avg60=3.00 avg300=1.00 total=99\n")
	put("proc/vmstat", "nr_free_pages 1\noom_kill 3\n")
	proc := func(pid, comm string, rssKB int, args ...string) {
		put("proc/"+pid+"/comm", comm+"\n")
		put("proc/"+pid+"/cmdline", strings.Join(args, "\x00")+"\x00")
		put("proc/"+pid+"/status", "Name:\t"+comm+"\nVmRSS:\t   "+strconv.Itoa(rssKB)+" kB\n")
	}
	task := "--log=/run/containerd/io.containerd.runtime.v2.task/labs/lab-"
	proc("10", "gvisor_sentry", 46080, "runsc-sandbox", "--root=/run/containerd/runsc/labs", task+idA+"/log.json", "boot")
	proc("11", "exe", 24576, "runsc-gofer", task+idA+"/log.json", "gofer")
	proc("12", "containerd-shim", 24576, "/usr/local/bin/containerd-shim-runsc-v1", "-namespace", "labs", "-id", "lab-"+idA)
	proc("20", "gvisor_sentry", 51200, "runsc-sandbox", task+idB+"/log.json", "boot")
	proc("30", "gvisor_sentry", 99999, "runsc-sandbox", "--log=/run/.../labs/c-debug/log.json") // not a lab
	proc("40", "containerd", 45056, "/usr/local/bin/containerd")
	proc("50", "bash", 4096, "bash")
	put("proc/60/cmdline", "") // a kernel thread
	cg := "sys/fs/cgroup/labs/"
	put(cg+"lab-"+idA+"/memory.current", "29360128\n") // 28 MiB
	put(cg+"lab-"+idA+"/cpu.stat", "usage_usec 1000000\nuser_usec 600000\n")
	put(cg+"lab-"+idB+"/memory.current", "31457280\n") // 30 MiB
	put(cg+"lab-"+idB+"/cpu.stat", "usage_usec 5000000\n")
	put(cg+"c-debug/memory.current", "1\n")
	put(cg+"memory.current", "999\n")
	return root
}

func TestCollector_ParsesHost(t *testing.T) {
	t.Parallel()
	root := fakeHost(t)
	fifos := t.TempDir()
	_ = os.WriteFile(filepath.Join(fifos, "123"), nil, 0o644)
	c := &Collector{Root: root, CgroupDir: "sys/fs/cgroup/labs", FIFODir: fifos,
		Containers: func(context.Context) (int, error) { return 2, nil },
		Stats: func(context.Context) (LabdStats, error) {
			var s LabdStats
			s.Active, s.Running = 2, 2
			s.Labd.Goroutines = 40
			return s, nil
		}}
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s := c.Sample(context.Background(), t0, true)

	if s.MemUsedMB != 3906.3 || s.MemAvailMB != 11718.8 {
		t.Errorf("memory used %v available %v", s.MemUsedMB, s.MemAvailMB)
	}
	if s.PSI.Memory != 1.5 || s.PSI.CPU != 12.25 || s.PSI.IO != 0 {
		t.Errorf("psi %+v", s.PSI)
	}
	if s.OOMKills != 3 || s.Containers != 2 || s.FIFOs != 1 || s.Stats.Labd.Goroutines != 40 {
		t.Errorf("oom %d containers %d fifos %d stats %+v", s.OOMKills, s.Containers, s.FIFOs, s.Stats)
	}
	if s.ContainerdRSSMB != 44 {
		t.Errorf("containerd rss %v", s.ContainerdRSSMB)
	}
	if s.LabCount != 2 || s.LabMemMB.P50 != 28 || s.LabMemMB.Max != 30 || s.LabMemSumMB != 58 {
		t.Errorf("labs %d mem %+v sum %v", s.LabCount, s.LabMemMB, s.LabMemSumMB)
	}
	byID := map[string]LabSample{}
	for _, l := range s.Labs {
		byID[l.SessionID] = l
	}
	if a := byID[idA]; a.SentryMB != 45 || a.HostRSSMB != 93 || a.CPUPct != -1 {
		t.Errorf("lab A %+v", a)
	}
	if b := byID[idB]; b.SentryMB != 50 || b.HostRSSMB != 50 {
		t.Errorf("lab B %+v", b)
	}
	if s.CPUPct != 0 {
		t.Errorf("host cpu %v on the first sample, want 0 (no delta yet)", s.CPUPct)
	}

	// Second tick 10 s later: lab A used 1 s of CPU (10 % of a core), the host 300 of 1000
	// jiffies busy.
	put := func(p, v string) { _ = os.WriteFile(filepath.Join(root, p), []byte(v), 0o644) }
	put("sys/fs/cgroup/labs/lab-"+idA+"/cpu.stat", "usage_usec 2000000\n")
	put("proc/stat", "cpu  1200 0 600 8600 600 0 0 0 0 0\n")
	s = c.Sample(context.Background(), t0.Add(10*time.Second), false)
	if s.T != 10 || s.Containers != -1 {
		t.Errorf("t %v containers %d", s.T, s.Containers)
	}
	if s.CPUPct != 30 {
		t.Errorf("host cpu %v, want 30", s.CPUPct)
	}
	for _, l := range s.Labs {
		if l.SessionID == idA && l.CPUPct != 10 {
			t.Errorf("lab A cpu %v %%, want 10", l.CPUPct)
		}
		if l.SessionID == idB && l.CPUPct != 0 {
			t.Errorf("lab B cpu %v %%, want 0", l.CPUPct)
		}
	}
	if s.LabCPUPctSum != 10 || s.LabCPUPct.Max != 10 {
		t.Errorf("lab cpu %+v sum %v", s.LabCPUPct, s.LabCPUPctSum)
	}
}

func TestCollector_MissingFilesAreZero(t *testing.T) {
	t.Parallel()
	c := &Collector{Root: t.TempDir(), CgroupDir: "nope"}
	s := c.Sample(context.Background(), time.Now(), true)
	if s.MemUsedMB != 0 || s.LabCount != 0 || s.Containers != -1 {
		t.Errorf("%+v", s)
	}
}
