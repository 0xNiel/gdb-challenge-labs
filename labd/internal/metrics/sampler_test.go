package metrics

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gdblabs/labd/internal/store"
)

const sid = "0a1b2c3d-1111-2222-3333-444455556666"

type fakeSrc struct {
	running        []string
	active, queued int
}

func (f fakeSrc) Running() []string            { return f.running }
func (f fakeSrc) Counts() (active, queued int) { return f.active, f.queued }

// tree writes a fake /proc and cgroup tree under root.
type tree struct {
	t    *testing.T
	root string
}

func (tr tree) write(rel, body string) {
	tr.t.Helper()
	p := filepath.Join(tr.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		tr.t.Fatal(err)
	}
}

func newTree(t *testing.T) tree {
	tr := tree{t, t.TempDir()}
	tr.write("proc/meminfo", "MemTotal:       16000000 kB\nMemFree:  1 kB\nMemAvailable:   12000000 kB\n")
	tr.write("proc/stat", "cpu  100 0 100 800 0 0 0 0 0 0\ncpu0 1 1 1 1\n")
	tr.write("proc/pressure/cpu", "some avg10=12.50 avg60=1.00 avg300=0.00 total=1\nfull avg10=0.00 avg60=0.00 avg300=0.00 total=0\n")
	tr.write("proc/pressure/memory", "some avg10=0.25 avg60=0.00 avg300=0.00 total=1\n")
	tr.write("sys/fs/cgroup/labs/lab-"+sid+"/memory.current", "27262976\n") // 26 MiB
	tr.write("sys/fs/cgroup/labs/lab-"+sid+"/cpu.stat", "usage_usec 1000000\nuser_usec 1\n")
	tr.write("proc/4242/cmdline", "runsc-sandbox\x00--root=/run\x00boot\x00lab-"+sid+"\x00")
	tr.write("proc/4242/status", "Name: runsc-sandbox\nVmRSS:     46080 kB\n") // 45 MiB
	return tr
}

func byMetric(ss []store.Sample) map[string]store.Sample {
	out := map[string]store.Sample{}
	for _, s := range ss {
		out[s.Metric+"/"+s.SessionID] = s
	}
	return out
}

func TestSampler_Ticks(t *testing.T) {
	t.Parallel()
	tr := newTree(t)
	ws := map[string][2]int64{sid: {100, 5000}}
	var peak float64
	s := &Sampler{
		Root: tr.root, CgroupDir: "sys/fs/cgroup/labs",
		Src:     fakeSrc{running: []string{sid}, active: 3, queued: 1},
		WSBytes: func() map[string][2]int64 { return ws },
		OnRSS:   func(id string, mb float64) { peak = mb },
	}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	m := byMetric(s.Tick(t0))
	want := map[string]float64{
		HostMemUsedMB + "/": 3906.3, HostCPUPressure + "/": 12.5, HostMemPressure + "/": 0.25,
		LabdActive + "/": 3, LabdQueued + "/": 1,
		SessionRSSMB + "/" + sid: 26, SessionSentryMB + "/" + sid: 45,
		WSBytesIn + "/" + sid: 100, WSBytesOut + "/" + sid: 5000,
	}
	for k, v := range want {
		if got, ok := m[k]; !ok || got.Value != v || !got.TS.Equal(t0) {
			t.Errorf("tick 1 %s = %+v (present %v), want %v", k, got, ok, v)
		}
	}
	for _, k := range []string{HostCPUPct + "/", SessionCPUMS + "/" + sid} {
		if _, ok := m[k]; ok {
			t.Errorf("tick 1 has %s: a delta needs a previous reading", k)
		}
	}
	if peak != 26 {
		t.Errorf("OnRSS got %v", peak)
	}

	// 10 s later: 1000 more jiffies, 250 of them busy; the lab used 1.5 s of CPU.
	tr.write("proc/stat", "cpu  200 0 250 1550 0 0 0 0 0 0\n")
	tr.write("sys/fs/cgroup/labs/lab-"+sid+"/cpu.stat", "usage_usec 2500000\n")
	ws = map[string][2]int64{sid: {130, 9000}}
	m = byMetric(s.Tick(t0.Add(10 * time.Second)))
	for k, v := range map[string]float64{
		HostCPUPct + "/": 25, SessionCPUMS + "/" + sid: 1500, WSBytesIn + "/" + sid: 30, WSBytesOut + "/" + sid: 4000,
	} {
		if got := m[k].Value; got != v {
			t.Errorf("tick 2 %s = %v, want %v", k, got, v)
		}
	}
}

func TestSampler_NoLabs(t *testing.T) {
	t.Parallel()
	tr := newTree(t)
	s := &Sampler{Root: tr.root, CgroupDir: "sys/fs/cgroup/labs", Src: fakeSrc{}}
	for _, x := range s.Tick(time.Now()) {
		if x.SessionID != "" {
			t.Errorf("per-session sample with no running lab: %+v", x)
		}
	}
	// A running lab whose cgroup is not there yet is skipped, not an error.
	s.Src = fakeSrc{running: []string{"ffffffff-1111-2222-3333-444455556666"}}
	for _, x := range s.Tick(time.Now()) {
		if x.SessionID != "" {
			t.Errorf("sample for a lab with no cgroup: %+v", x)
		}
	}
}

func TestSampler_RunWritesBatches(t *testing.T) {
	t.Parallel()
	tr := newTree(t)
	db := store.NewMemory()
	s := &Sampler{Root: tr.root, CgroupDir: "sys/fs/cgroup/labs", Src: fakeSrc{running: []string{sid}, active: 1}, Sink: db}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx, 10*time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		names := map[string]bool{}
		for _, x := range db.Samples() {
			names[x.Metric] = true
		}
		if names[HostCPUPct] && names[SessionCPUMS] { // both need a second tick
			cancel()
			<-done
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatalf("no second tick written: %d samples", len(db.Samples()))
}

func TestReaders(t *testing.T) {
	t.Parallel()
	tr := newTree(t)
	if c := ReadCPU(filepath.Join(tr.root, "proc/stat")); c.Total != 1000 || c.Idle != 800 {
		t.Errorf("cpu %+v", c)
	}
	if got := CPUPct(CPUTimes{}, CPUTimes{Total: 10}); got != 0 {
		t.Errorf("CPUPct with no previous reading = %v", got)
	}
	p := ScanProcs(filepath.Join(tr.root, "proc"))
	if p.Sentry[sid] != 45 {
		t.Errorf("sentry %v", p.Sentry)
	}
	if ReadPSI(filepath.Join(tr.root, "proc/pressure/io")) != 0 {
		t.Error("absent PSI file should read 0")
	}
}
