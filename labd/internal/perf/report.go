package perf

// Report is perf-report-<date>-<host>.json. The five top-level objects are the spec's schema
// exactly ("Local performance test suite → Outputs"); report_test.go checks the key set
// against the spec itself. Meta and Extra are additions: where each number came from, and
// the plan's "Metrics to record" that the spec's schema has no key for.
type Report struct {
	PerLab         PerLab         `json:"per_lab"`
	HostAt100      HostAt100      `json:"host_at_100"`
	GVisorOverhead GVisorOverhead `json:"gvisor_overhead"`
	Leaks          Leaks          `json:"leaks"`
	Derived        Derived        `json:"derived"`

	Meta  *ReportMeta    `json:"meta,omitempty"`
	Extra map[string]any `json:"extra,omitempty"`
}

// P50P95Max is the spec's {"p50","p95","max"}.
type P50P95Max struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

// P50P95 is the spec's {"p50","p95"}.
type P50P95 struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
}

type PerLab struct {
	RSSMB     P50P95Max `json:"rss_mb"`
	CPUPctAvg float64   `json:"cpu_pct_avg"`
	DiskMB    float64   `json:"disk_mb"`
	WSBpsIn   float64   `json:"ws_bps_in"`
	WSBpsOut  float64   `json:"ws_bps_out"`
}

type HostAt100 struct {
	MemUsedMB      float64 `json:"mem_used_mb"`
	CPUPct         float64 `json:"cpu_pct"`
	StartLatencyMS P50P95  `json:"start_latency_ms"`
	EchoLatencyMS  P50P95  `json:"echo_latency_ms"`
}

type GVisorOverhead struct {
	SentryRSSMB    float64 `json:"sentry_rss_mb"`
	StepCmdMSRunc  float64 `json:"step_cmd_ms_runc"`
	StepCmdMSRunsc float64 `json:"step_cmd_ms_runsc"`
}

type Leaks struct {
	Containers int `json:"containers"`
	Goroutines int `json:"goroutines"`
	FIFOs      int `json:"fifos"`
}

type Derived struct {
	MaxSessionsAt25pctHeadroom int `json:"max_sessions_at_25pct_headroom"`
}

// ReportMeta says where the report's numbers came from.
type ReportMeta struct {
	Host    string            `json:"host"`
	Arch    string            `json:"arch"`
	Date    string            `json:"date"`
	Sources map[string]string `json:"sources"` // report field -> run file it came from
	Partial []string          `json:"partial,omitempty"`
	Notes   []string          `json:"notes,omitempty"`
}
