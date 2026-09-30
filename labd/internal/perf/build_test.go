package perf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeRun(t *testing.T, dir, name string, rf RunFile) {
	t.Helper()
	b, _ := json.Marshal(rf)
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReport_FromRuns(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	meta := func(sc string, n int, date string) RunMeta {
		return RunMeta{Scenario: sc, Host: "lap", Arch: "x86_64", N: n, Date: date}
	}
	p2 := RunFile{Meta: meta("P2", 100, "2026-10-02T10:00:00Z"), Summary: RunSummary{
		LabMemMB: Pctl{P50: 26, P95: 28, Max: 31}, HostPerLabMB: 40, HostBaselineMB: 3000, HostMemUsedMB: P50P95Max{Max: 7000},
		SentryRSSMB: Pctl{P50: 45}, LabCPUPctAvg: 0.2, StartToPromptMS: Pctl{P50: 1500, P95: 1900}}}
	p2.Extra = map[string]any{"disk": []map[string]any{
		{"t": 0, "snapshots_mb": 100.0, "active": 0},
		{"t": 300, "snapshots_mb": 150.0, "active": 100, "images_mib": map[string]any{"labbase": 93.2, "perf": 94.3}},
	}}
	writeRun(t, dir, "run-P2-2026-10-02-lap.json", p2)
	old := p2
	old.Meta.Date = "2026-10-01T10:00:00Z"
	old.Summary.LabMemMB.P95 = 999
	writeRun(t, dir, "run-P2-2026-10-01-lap.json", old) // older: ignored
	writeRun(t, dir, "run-P3-2026-10-02-lap.json", RunFile{Meta: meta("P3", 100, "2026-10-02T11:00:00Z"),
		Summary: RunSummary{EchoMS: Pctl{P50: 3, P95: 9}, HostCPUPct: P50P95Max{P50: 30}},
		Criteria: []Criterion{{Criterion: "x", Pass: false}}})
	writeRun(t, dir, "run-P8-2026-10-02-lap.json", RunFile{Meta: meta("P8", 60, "2026-10-02T12:00:00Z"),
		Summary: RunSummary{WSBpsIn: 4, WSBpsOut: 60, WSBpsOutAggregate: 3600}})
	writeRun(t, dir, "run-P1-2026-10-02-lap.json", RunFile{Meta: meta("P1", 1, "2026-10-02T09:00:00Z"), Summary: RunSummary{StepCmdMS: Pctl{P50: 12}}})
	writeRun(t, dir, "run-P1-2026-10-02-lap-runc.json", RunFile{Meta: meta("P1", 1, "2026-10-02T09:30:00Z"), Summary: RunSummary{StepCmdMS: Pctl{P50: 3}}})
	writeRun(t, dir, "run-P4-2026-10-02-lap.json", RunFile{Meta: meta("P4", 100, "2026-10-02T13:00:00Z"), Summary: RunSummary{Leaks: Leaks{Goroutines: 1}}})
	writeRun(t, dir, "run-P2-2026-10-02-other.json", RunFile{Meta: meta("P2", 100, "2026-10-03T00:00:00Z")}) // another host

	runs, err := LoadRuns(dir, "lap")
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 6 || runs["P2"].File != "run-P2-2026-10-02-lap.json" || runs["P1-runc"].File == "" {
		t.Fatalf("runs %v", sortedKeys(runs))
	}
	rep, missing := BuildReport(runs)
	if rep.PerLab.RSSMB.P95 != 28 || rep.PerLab.DiskMB != 0.5 || rep.PerLab.WSBpsOut != 60 {
		t.Errorf("per lab %+v", rep.PerLab)
	}
	// floor((32768 * 0.75 - 3000) / max(28, 40)) = floor(21576 / 40) = 539
	if rep.Derived.MaxSessionsAt25pctHeadroom != 539 {
		t.Errorf("derived %d", rep.Derived.MaxSessionsAt25pctHeadroom)
	}
	if rep.GVisorOverhead.StepCmdMSRunsc != 12 || rep.GVisorOverhead.StepCmdMSRunc != 3 || rep.HostAt100.EchoLatencyMS.P95 != 9 {
		t.Errorf("overhead %+v host %+v", rep.GVisorOverhead, rep.HostAt100)
	}
	if rep.Leaks.Goroutines != 1 || rep.Meta.Sources["per_lab.rss_mb"] != "run-P2-2026-10-02-lap.json" {
		t.Errorf("leaks %+v sources %v", rep.Leaks, rep.Meta.Sources)
	}
	if len(rep.Meta.Partial) != 1 || !strings.Contains(rep.Meta.Partial[0], "P8 ran at N=60") {
		t.Errorf("partial %v", rep.Meta.Partial)
	}
	if len(missing) != 0 {
		t.Errorf("missing %v", missing)
	}

	md := "intro\n" + BlockBegin + "\nold\n" + BlockEnd + "\n## Failures and decisions\nkeep me\n"
	out, err := ReplaceBlock(md, RenderCapacity(rep, runs, missing))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"intro\n", "keep me", "**539**", "run-P2-2026-10-02-lap.json", "about 313 MiB", "**miss**"} {
		if !strings.Contains(out, want) {
			t.Errorf("capacity block lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\nold\n") || strings.Contains(out, "est.") {
		t.Errorf("old block or an estimate left:\n%s", out)
	}
	if _, err := ReplaceBlock("no markers", "x"); err == nil {
		t.Error("ReplaceBlock accepted a file without markers")
	}
}
