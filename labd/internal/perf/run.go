package perf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RunConfig is one labd-perf run.
type RunConfig struct {
	Scenario string        // P1..P9
	N        int           // sessions (P6: requests)
	Ramp     float64       // sessions started per second
	Hold     time.Duration // P1-P3, P8: per user once gdb is up; P4, P9: churn time; P5: after recovery
	Mix      map[string]int

	API, WS, Secret, Challenge string
	Profiles                   map[string]Profile

	Host, Runtime, Platform, Label string // labels for the file name and meta
	Partial                        string // why N is below the spec's (lack of RAM), if it is
	Out                            string // directory for run-*.json
	Command                        string // the command line, for meta

	Root       string // "/" on the lab host (collector)
	CgroupDir  string
	FIFODir    string
	Containers func(ctx context.Context) (int, error)

	Churn     time.Duration // P4/P9: one session ends and one starts this often (spec 5 s)
	DiskCmd   string        // prints disk usage as JSON (scenario.sh --disk); "" skips it
	DiskEvery time.Duration
	ReadyFile string // P5: written once the sessions are up; the wrapper then kills labd
	PullCmd   string // P7: makes the flushed image available again (the "pull")

	Settle time.Duration // wait after every session runs, before the hold window (default 30 s)
	Log    io.Writer
}

// RunFile is run-<scenario>-<date>-<host>[-label].json.
type RunFile struct {
	Meta     RunMeta        `json:"meta"`
	Summary  RunSummary     `json:"summary"`
	Criteria []Criterion    `json:"criteria"`
	Timeline []Mark         `json:"timeline"`
	Extra    map[string]any `json:"extra,omitempty"`
	VUsers   []VUserResult  `json:"vusers"`
	Samples  []Sample       `json:"samples"`
}

type RunMeta struct {
	Scenario    string         `json:"scenario"`
	Host        string         `json:"host"`
	Arch        string         `json:"arch"`
	Kernel      string         `json:"kernel"`
	Runtime     string         `json:"runtime"`
	Platform    string         `json:"platform,omitempty"`
	Runsc       string         `json:"runsc,omitempty"`
	Label       string         `json:"label,omitempty"`
	Date        string         `json:"date"`
	N           int            `json:"n"`
	Ramp        float64        `json:"ramp_per_s"`
	HoldS       int            `json:"hold_s"`
	Mix         map[string]int `json:"mix"`
	MaxSessions int            `json:"max_sessions"`
	HostMemMB   float64        `json:"host_mem_total_mb"`
	CPUs        int            `json:"cpus"`
	Partial     string         `json:"partial,omitempty"`
	Command     string         `json:"command"`
	DurationS   float64        `json:"duration_s"`
}

// Mark is a point on the run's timeline (seconds since start).
type Mark struct {
	T     float64 `json:"t"`
	Event string  `json:"event"`
}

// Criterion is one spec pass criterion, evaluated.
type Criterion struct {
	Criterion string `json:"criterion"`
	Measured  string `json:"measured"`
	Pass      bool   `json:"pass"`
}

// RunSummary holds the numbers the report and capacity.md use.
type RunSummary struct {
	Sessions        int             `json:"sessions"`
	VUserErrors     int             `json:"vuser_errors"`
	Commands        int             `json:"commands"`
	CommandErrors   int             `json:"command_errors"`
	StartToPromptMS Pctl            `json:"start_to_prompt_ms"`
	CreateHTTPMS    Pctl            `json:"create_http_ms"`
	QueueWaitMS     Pctl            `json:"queue_wait_ms"`
	EchoMS          Pctl            `json:"echo_ms"`
	EchoMSByProfile map[string]Pctl `json:"echo_ms_by_profile"`
	StepCmdMS       Pctl            `json:"step_cmd_ms"` // next and step: send to gdb's prompt
	CmdMS           map[string]Pctl `json:"cmd_ms"`

	HoldLabs           int                `json:"hold_labs"` // labs running during the hold window
	HoldSamples        int                `json:"hold_samples"`
	LabMemMB           Pctl               `json:"lab_mem_mb"` // cgroup memory.current, every lab, every hold sample
	SentryRSSMB        Pctl               `json:"sentry_rss_mb"`
	LabHostRSSMB       Pctl               `json:"lab_host_rss_mb"` // sandbox + gofer + shim RSS (shared pages counted)
	LabCPUPctAvg       float64            `json:"lab_cpu_pct_avg"` // % of one core, mean over labs and hold samples
	LabCPUPctByProfile map[string]float64 `json:"lab_cpu_pct_by_profile"`
	HostBaselineMB     float64            `json:"host_baseline_mb"` // idle host: the lower of before the run and after teardown
	HostAfterMB        float64            `json:"host_after_mb"`    // host memory used after every session ended
	HostMemUsedMB      P50P95Max          `json:"host_mem_used_mb"` // during the hold
	HostPerLabMB       float64            `json:"host_per_lab_mb"`  // (peak used - baseline) / labs: the whole cost of a lab
	HostCPUPct         P50P95Max          `json:"host_cpu_pct"`
	PSIMax             PSI                `json:"psi_max"`
	OOMKills           int64              `json:"oom_kills"`
	LabdRSSMB          Triple             `json:"labd_rss_mb"`
	LabdGoroutines     Triple             `json:"labd_goroutines"`
	ContainerdRSSMB    Triple             `json:"containerd_rss_mb"`
	ContainersMax      int                `json:"containers_max"`
	WSBpsIn            float64            `json:"ws_bps_in"`  // per session, keystrokes
	WSBpsOut           float64            `json:"ws_bps_out"` // per session, terminal output
	WSBpsOutAggregate  float64            `json:"ws_bps_out_aggregate"`
	Leaks              Leaks              `json:"leaks"`
}

// Triple is a value idle (before), at the peak and after everything ended.
type Triple struct {
	Idle  float64 `json:"idle"`
	Peak  float64 `json:"peak"`
	After float64 `json:"after"`
}

type runner struct {
	cfg     RunConfig
	t0      time.Time
	col     *Collector
	mu      sync.Mutex
	samples []Sample
	marks   []Mark
	users   []VUserResult
	extra   map[string]any
	hold    [2]float64 // hold window, seconds since t0
	nextUID atomic.Int64
	http    *http.Client
	idle    Sample
}

func (r *runner) logf(format string, a ...any) {
	fmt.Fprintf(r.cfg.Log, "labd-perf %s t=%.0fs: %s\n", r.cfg.Scenario, time.Since(r.t0).Seconds(), fmt.Sprintf(format, a...))
}

func (r *runner) mark(ev string) {
	r.mu.Lock()
	r.marks = append(r.marks, Mark{T: round(time.Since(r.t0).Seconds(), 1), Event: ev})
	r.mu.Unlock()
	r.logf("%s", ev)
}

func (r *runner) now() float64 { return round(time.Since(r.t0).Seconds(), 1) }

// Run runs one scenario against a running labd and writes its run file.
func Run(ctx context.Context, cfg RunConfig) (string, RunFile, error) {
	if cfg.Log == nil {
		cfg.Log = os.Stderr
	}
	if cfg.Churn == 0 {
		cfg.Churn = 5 * time.Second
	}
	if cfg.DiskEvery == 0 {
		cfg.DiskEvery = 5 * time.Minute
	}
	if cfg.Settle == 0 {
		cfg.Settle = 30 * time.Second
	}
	r := &runner{cfg: cfg, t0: time.Now(), extra: map[string]any{}, http: &http.Client{Timeout: 30 * time.Second}}
	r.nextUID.Store(time.Now().Unix() % 100000 * 1000) // distinct users across runs
	r.col = &Collector{Root: cfg.Root, CgroupDir: cfg.CgroupDir, FIFODir: cfg.FIFODir, Containers: cfg.Containers, Stats: r.stats}

	st, err := r.stats(ctx)
	if err != nil {
		return "", RunFile{}, fmt.Errorf("labd not reachable at %s: %w", cfg.API, err)
	}
	if st.Active != 0 || st.Queued != 0 {
		return "", RunFile{}, fmt.Errorf("labd already has %d active and %d queued sessions; start from none", st.Active, st.Queued)
	}
	r.idle = r.baseline(ctx)
	r.mark("baseline taken")

	if cfg.DiskCmd != "" {
		r.disk(ctx, "start") // before the first session, so growth is measured from nothing
	}
	sctx, stopSampling := context.WithCancel(ctx)
	var sampling sync.WaitGroup
	sampling.Add(1)
	go func() { defer sampling.Done(); r.sampleLoop(sctx) }()
	if cfg.DiskCmd != "" {
		sampling.Add(1)
		go func() { defer sampling.Done(); r.diskLoop(sctx) }()
	}

	var body error
	switch cfg.Scenario {
	case "P1", "P2", "P3", "P8":
		body = r.steady(ctx)
	case "P4", "P9":
		body = r.churn(ctx)
	case "P5":
		body = r.recovery(ctx)
	case "P6":
		body = r.overload(ctx)
	case "P7":
		body = r.coldStart(ctx)
	default:
		body = fmt.Errorf("unknown scenario %q", cfg.Scenario)
	}
	if body != nil {
		r.logf("scenario failed: %v", body)
		r.extra["error"] = body.Error()
	}
	r.teardown(ctx)
	stopSampling()
	sampling.Wait()
	if cfg.DiskCmd != "" {
		r.disk(ctx, "end")
	}

	rf := r.file(ctx)
	path, werr := r.write(rf)
	if werr != nil {
		return "", rf, werr
	}
	if body != nil {
		return path, rf, body
	}
	return path, rf, nil
}

func (r *runner) stats(ctx context.Context) (LabdStats, error) {
	var st LabdStats
	err := r.api(ctx, http.MethodGet, "/internal/stats", nil, &st)
	return st, err
}

// api calls labd's internal API and decodes a JSON reply into out (if not nil).
func (r *runner) api(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, r.cfg.API+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s %s: HTTP %d %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// baseline samples the idle host three times over 10 s and keeps the median memory.
func (r *runner) baseline(ctx context.Context) Sample {
	var ss []Sample
	for i := 0; i < 3; i++ {
		if i > 0 {
			_ = sleepCtx(ctx, 5*time.Second)
		}
		ss = append(ss, r.col.Sample(ctx, time.Now(), true))
	}
	slices.SortFunc(ss, func(a, b Sample) int { return int(a.MemUsedMB - b.MemUsedMB) })
	b := ss[1]
	r.mu.Lock()
	r.samples = append(r.samples, ss...)
	r.mu.Unlock()
	return b
}

func (r *runner) sampleLoop(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for i := 1; ; i++ {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			s := r.col.Sample(ctx, now, i%6 == 0)
			r.mu.Lock()
			r.samples = append(r.samples, s)
			r.mu.Unlock()
		}
	}
}

// diskLoop runs DiskCmd every DiskEvery (P9: snapshot, journal and table growth).
func (r *runner) diskLoop(ctx context.Context) {
	tick := time.NewTicker(r.cfg.DiskEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			r.disk(ctx, "")
		}
	}
}

// diskAtN samples disk once every session runs, so per-lab growth is measured even when the
// hold is shorter than DiskEvery.
func (r *runner) diskAtN(ctx context.Context) {
	if r.cfg.DiskCmd != "" {
		r.disk(ctx, "at_n")
	}
}

func (r *runner) disk(ctx context.Context, tag string) {
	out, err := exec.CommandContext(context.WithoutCancel(ctx), "bash", "-c", r.cfg.DiskCmd).Output()
	var d map[string]any
	if err == nil {
		err = json.Unmarshal(out, &d)
	}
	if err != nil {
		r.logf("disk sample failed: %v", err)
		return
	}
	d["t"] = r.now()
	if tag != "" {
		d["tag"] = tag
	}
	if st, err := r.stats(ctx); err == nil {
		d["active"] = st.Active
	}
	r.mu.Lock()
	ds, _ := r.extra["disk"].([]map[string]any)
	r.extra["disk"] = append(ds, d)
	r.mu.Unlock()
}

// assignProfiles spreads Mix over n users, interleaved so that every prefix keeps the
// ratio (P3: 60/30/10 means reader, reader, stepper, reader, ... with an abuser every 10th).
func assignProfiles(mix map[string]int, n int) []string {
	names := []string{Reader, Stepper, Abuser}
	total := 0
	for _, name := range names {
		total += mix[name]
	}
	out := make([]string, n)
	if total == 0 {
		for i := range out {
			out[i] = Reader
		}
		return out
	}
	have := map[string]int{}
	for i := range out {
		best, bestGap := "", -1e9
		for _, name := range names {
			if mix[name] == 0 {
				continue
			}
			gap := float64(mix[name])/float64(total)*float64(i+1) - float64(have[name])
			if gap > bestGap {
				best, bestGap = name, gap
			}
		}
		out[i] = best
		have[best]++
	}
	return out
}

func (r *runner) vcfg(profile string, hold time.Duration) VUserConfig {
	uid := r.nextUID.Add(1)
	return VUserConfig{
		API: r.cfg.API, WS: r.cfg.WS, Secret: r.cfg.Secret, UserID: uid, Challenge: r.cfg.Challenge,
		Profile: r.cfg.Profiles[profile], Hold: hold, Seed: uint64(uid), HTTP: r.http,
		QueueWait: 5 * time.Minute,
	}
}

// launch starts one user in the background; done receives its result.
func (r *runner) launch(ctx context.Context, cfg VUserConfig, wg *sync.WaitGroup) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := RunVUser(ctx, cfg)
		if res.Err != "" {
			r.logf("user %d (%s): %s", res.UserID, res.Profile, res.Err)
		}
		r.mu.Lock()
		r.users = append(r.users, res)
		r.mu.Unlock()
	}()
}

func (r *runner) finished() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.users)
}

// waitPrompts returns once want sessions are running (or their users are done), or at limit.
func (r *runner) waitPrompts(ctx context.Context, want int, limit time.Duration) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		// Users that already finished (a short hold) count as having got there.
		st, err := r.stats(ctx)
		if err == nil && st.Running+r.finished() >= want {
			return
		}
		_ = sleepCtx(ctx, time.Second)
	}
}

// steady is P1, P2, P3 and P8: ramp to N, hold, stop. User i holds (N-1-i)/ramp longer
// than the last, so all finish together and the hold window has all N running.
func (r *runner) steady(ctx context.Context) error {
	var wg sync.WaitGroup
	gap := time.Duration(float64(time.Second) / r.cfg.Ramp)
	r.mark(fmt.Sprintf("ramp: %d sessions at %.1f/s", r.cfg.N, r.cfg.Ramp))
	profiles := assignProfiles(r.cfg.Mix, r.cfg.N)
	for i := 0; i < r.cfg.N; i++ {
		extra := time.Duration(r.cfg.N-1-i) * gap
		r.launch(ctx, r.vcfg(profiles[i], r.cfg.Hold+extra), &wg)
		if sleepCtx(ctx, gap) != nil {
			break
		}
	}
	r.waitPrompts(ctx, r.cfg.N, 5*time.Minute)
	_ = sleepCtx(ctx, r.cfg.Settle) // gdb started and setup sent everywhere
	r.hold[0] = r.now()
	r.mark("hold start: all sessions running")
	r.diskAtN(ctx)
	// The first user to finish ends the window.
	for r.finished() == 0 && sleepCtx(ctx, time.Second) == nil {
	}
	r.mark("hold end: first session finished")
	wg.Wait()
	return nil
}

// churn is P4 and P9: ramp to N, then every Churn one random session ends and a new one
// starts, for Hold. Users are readers with no hold limit of their own.
func (r *runner) churn(ctx context.Context) error {
	var wg sync.WaitGroup
	type live struct {
		cancel context.CancelFunc
	}
	var lives []live
	start := func() {
		c, cancel := context.WithCancel(ctx)
		lives = append(lives, live{cancel})
		r.launch(c, r.vcfg(Reader, 24*time.Hour), &wg)
	}
	gap := time.Duration(float64(time.Second) / r.cfg.Ramp)
	for i := 0; i < r.cfg.N; i++ {
		start()
		_ = sleepCtx(ctx, gap)
	}
	r.waitPrompts(ctx, r.cfg.N, 5*time.Minute)
	r.hold[0] = r.now()
	r.diskAtN(ctx)
	r.mark(fmt.Sprintf("churn start: one session out and one in every %s for %s", r.cfg.Churn, r.cfg.Hold))
	end := time.Now().Add(r.cfg.Hold)
	rng := rand.New(rand.NewPCG(1, 2))
	var mismatch []string
	for time.Now().Before(end) && ctx.Err() == nil {
		i := rng.IntN(len(lives))
		lives[i].cancel()
		lives = slices.Delete(lives, i, i+1)
		start()
		if sleepCtx(ctx, r.cfg.Churn) != nil {
			break
		}
		// Every counted sample: containers never above the cap.
		r.mu.Lock()
		if s := r.samples[len(r.samples)-1]; s.Containers > r.cfg.N {
			mismatch = append(mismatch, fmt.Sprintf("t=%.0f containers %d > %d", s.T, s.Containers, r.cfg.N))
		}
		r.mu.Unlock()
	}
	r.hold[1] = r.now()
	r.mark("churn end")
	// Quiet: nothing creating or ending, then containers must equal active.
	var quiet string
	for i := 0; i < 60; i++ {
		st, err := r.stats(ctx)
		if err == nil && st.Creating+st.Ending+st.Queued == 0 {
			n := -1
			if r.cfg.Containers != nil {
				n, _ = r.cfg.Containers(ctx)
			}
			quiet = fmt.Sprintf("containers %d, active %d", n, st.Active)
			r.extra["quiet_containers"], r.extra["quiet_active"] = n, st.Active
			break
		}
		_ = sleepCtx(ctx, time.Second)
	}
	r.extra["over_cap"] = mismatch
	r.mark("quiet after churn: " + quiet)
	for _, l := range lives {
		l.cancel()
	}
	wg.Wait()
	return nil
}

// recovery is P5: N readers attached, then the wrapper kills labd with SIGKILL (it waits
// for ReadyFile) and restarts it. Measures time from labd answering again to every lab
// running, and whether every user reconnects to its own lab within the grace.
func (r *runner) recovery(ctx context.Context) error {
	if r.cfg.ReadyFile == "" {
		return errors.New("P5 needs --ready-file (run it through labd/perf/scenario.sh)")
	}
	var wg sync.WaitGroup
	hctx, stop := context.WithCancel(ctx)
	defer stop()
	gap := time.Duration(float64(time.Second) / r.cfg.Ramp)
	for i := 0; i < r.cfg.N; i++ {
		c := r.vcfg(Reader, 24*time.Hour)
		c.Reconnect = 90 * time.Second
		r.launch(hctx, c, &wg)
		_ = sleepCtx(ctx, gap)
	}
	r.waitPrompts(ctx, r.cfg.N, 5*time.Minute)
	_ = sleepCtx(ctx, 15*time.Second)
	r.hold[0] = r.now()
	if err := os.WriteFile(r.cfg.ReadyFile, []byte("ready\n"), 0o644); err != nil {
		return err
	}
	r.mark(fmt.Sprintf("%d sessions running; waiting for labd to be killed", r.cfg.N))
	down := r.waitAPI(ctx, false, 5*time.Minute)
	if down.IsZero() {
		return errors.New("labd was never killed")
	}
	r.mark("labd down")
	up := r.waitAPI(ctx, true, 2*time.Minute)
	if up.IsZero() {
		return errors.New("labd did not come back within 2 minutes")
	}
	r.mark("labd answering again")
	var adopted time.Time
	for time.Since(up) < time.Minute {
		if st, err := r.stats(ctx); err == nil && st.Running >= r.cfg.N {
			adopted = time.Now()
			break
		}
		_ = sleepCtx(ctx, 100*time.Millisecond)
	}
	if adopted.IsZero() {
		r.extra["adopted_ms"] = -1.0
		r.mark("not every lab was adopted within 60 s")
	} else {
		r.extra["adopted_ms"] = ms(adopted.Sub(up))
		r.mark(fmt.Sprintf("all %d labs running again %.0f ms after labd answered", r.cfg.N, ms(adopted.Sub(up))))
	}
	r.extra["down_ms"] = ms(up.Sub(down))
	if !adopted.IsZero() {
		// labd reconciles before it listens, so adoption is done by the time it answers: the
		// number that matters is from the kill to every lab running again.
		r.extra["recovered_ms"] = ms(adopted.Sub(down))
	}
	_ = sleepCtx(ctx, r.cfg.Hold)
	r.hold[1] = r.now()
	stop()
	wg.Wait()
	return nil
}

// waitAPI polls /healthz every 50 ms until it answers (up) or stops answering (!up).
func (r *runner) waitAPI(ctx context.Context, up bool, limit time.Duration) time.Time {
	c := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		resp, err := c.Get(r.cfg.API + "/healthz")
		ok := err == nil && resp.StatusCode == http.StatusOK
		if resp != nil {
			resp.Body.Close()
		}
		if ok == up {
			return time.Now()
		}
		_ = sleepCtx(ctx, 50*time.Millisecond)
	}
	return time.Time{}
}

type sessionRow struct {
	SessionID     string `json:"session_id"`
	UserID        int64  `json:"user_id"`
	State         string `json:"state"`
	QueuePosition int    `json:"queue_position"`
}

func (r *runner) sessions(ctx context.Context) ([]sessionRow, error) {
	var out struct {
		Sessions []sessionRow `json:"sessions"`
	}
	err := r.api(ctx, http.MethodGet, "/internal/sessions", nil, &out)
	return out.Sessions, err
}

// overload is P6: N requests (150) against the cap (100), no terminals. Checks the queue
// positions, that nothing runs over the cap, and that the queue drains first in, first out.
func (r *runner) overload(ctx context.Context) error {
	st, err := r.stats(ctx)
	if err != nil {
		return err
	}
	type req struct {
		uid                int64
		id, state          string
		pos                int
		queuedAt, admitted time.Time
	}
	reqs := make([]*req, r.cfg.N)
	var over []string
	watch := func() {
		if s, err := r.stats(ctx); err == nil && s.Active > st.MaxSessions {
			over = append(over, fmt.Sprintf("t=%.1f active %d", r.now(), s.Active))
		}
	}
	r.mark(fmt.Sprintf("%d requests against cap %d (queue %d)", r.cfg.N, st.MaxSessions, st.MaxQueue))
	var rejected int
	for i := range reqs {
		uid := r.nextUID.Add(1)
		var sr startResp
		b := fmt.Sprintf(`{"user_id":%d,"challenge_slug":%q}`, uid, r.cfg.Challenge)
		if err := r.api(ctx, http.MethodPost, "/internal/sessions", strings.NewReader(b), &sr); err != nil {
			rejected++
			reqs[i] = &req{uid: uid, state: "rejected: " + err.Error()}
			continue
		}
		reqs[i] = &req{uid: uid, id: sr.SessionID, state: sr.State, pos: sr.QueuePosition, queuedAt: time.Now()}
		if i%10 == 0 {
			watch()
		}
	}
	admittedN, queuedN, posOK := 0, 0, true
	for _, q := range reqs {
		switch q.state {
		case "creating", "running":
			admittedN++
		case "queued":
			queuedN++
			if q.pos != queuedN {
				posOK = false
			}
		}
	}
	r.mark(fmt.Sprintf("admitted %d, queued %d, rejected %d", admittedN, queuedN, rejected))
	// Wait until every admitted lab runs, then stop them one at a time and record the
	// order in which queued sessions are admitted.
	for i := 0; i < 120; i++ {
		if s, err := r.stats(ctx); err == nil && s.Running >= admittedN {
			break
		}
		_ = sleepCtx(ctx, time.Second)
	}
	r.hold[0] = r.now()
	var order []int64
	pending := map[string]*req{}
	for _, q := range reqs {
		if q.state == "queued" {
			pending[q.id] = q
		}
	}
	running := func() []string {
		var ids []string
		for _, q := range reqs {
			if (q.state == "creating" || q.state == "running") && q.admitted.IsZero() {
				ids = append(ids, q.id)
			}
		}
		return ids
	}()
	for _, id := range running {
		_ = r.api(ctx, http.MethodDelete, "/internal/sessions/"+id, strings.NewReader(`{"reason":"admin_kill"}`), nil)
		_ = sleepCtx(ctx, 200*time.Millisecond)
		rows, err := r.sessions(ctx)
		if err != nil {
			continue
		}
		watch()
		for _, row := range rows {
			if q, ok := pending[row.SessionID]; ok && row.State != "queued" {
				q.admitted = time.Now()
				order = append(order, q.uid)
				delete(pending, row.SessionID)
			}
		}
		if len(pending) == 0 {
			break
		}
	}
	r.hold[1] = r.now()
	fifo := true
	var want []int64
	var waits []float64
	for _, q := range reqs {
		if q.state == "queued" {
			want = append(want, q.uid)
			if !q.admitted.IsZero() {
				waits = append(waits, ms(q.admitted.Sub(q.queuedAt)))
			}
		}
	}
	// Admissions seen in the same poll may be listed in any order; compare as sequences of
	// the queue order with ties allowed only within one poll. A plain equality is stricter
	// and holds when polling is faster than admissions, which it is (200 ms).
	fifo = slices.Equal(order, want)
	r.extra["p6"] = map[string]any{
		"cap": st.MaxSessions, "max_queue": st.MaxQueue, "requests": r.cfg.N,
		"admitted": admittedN, "queued": queuedN, "rejected": rejected,
		"positions_in_order": posOK, "never_over_cap": len(over) == 0, "over_cap": over,
		"drained_fifo": fifo, "admitted_from_queue": len(order), "queue_wait_ms": Percentiles(waits),
	}
	r.mark(fmt.Sprintf("drain: %d of %d queued admitted, FIFO %v", len(order), queuedN, fifo))
	return nil
}

// coldStart is P7: the wrapper has removed the image from containerd. A start must fail
// (labd never pulls at request time, S20), then PullCmd brings the image back (timed), then
// N sessions start on a cold cache, then N again warm.
func (r *runner) coldStart(ctx context.Context) error {
	p7 := map[string]any{}
	r.extra["p7"] = p7
	res := RunVUser(ctx, r.vcfg(Reader, 0))
	p7["without_image"] = map[string]any{"err": res.Err, "ended": res.Ended}
	r.mark("start without the image: " + firstNonEmpty(res.Ended, res.Err, "it started (unexpected)"))
	if r.cfg.PullCmd == "" {
		return errors.New("P7 needs --pull-cmd (run it through labd/perf/scenario.sh)")
	}
	t := time.Now()
	out, err := exec.CommandContext(ctx, "bash", "-c", r.cfg.PullCmd).CombinedOutput()
	p7["pull_ms"] = ms(time.Since(t))
	if err != nil {
		return fmt.Errorf("pull command: %v: %s", err, out)
	}
	r.mark(fmt.Sprintf("image back after %.0f ms", ms(time.Since(t))))
	for _, phase := range []string{"after_pull", "warm"} {
		var wg sync.WaitGroup
		first := len(r.users)
		for i := 0; i < r.cfg.N; i++ {
			r.launch(ctx, r.vcfg(Reader, 20*time.Second), &wg)
		}
		wg.Wait()
		var st []float64
		r.mu.Lock()
		batch := slices.Clone(r.users[first:])
		r.mu.Unlock()
		for _, u := range batch {
			if u.StartToPromptMS > 0 {
				st = append(st, u.StartToPromptMS)
			}
		}
		p7[phase+"_start_to_prompt_ms"] = Percentiles(st)
		r.mark(fmt.Sprintf("%s: %d sessions, start to prompt p50 %.0f ms", phase, len(st), Percentiles(st).P50))
		r.waitIdle(ctx)
	}
	return nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// waitIdle waits until labd has no session left (60 s at most).
func (r *runner) waitIdle(ctx context.Context) bool {
	for i := 0; i < 120; i++ {
		if st, err := r.stats(ctx); err == nil && st.Active+st.Queued+st.Ending == 0 {
			return true
		}
		_ = sleepCtx(ctx, 500*time.Millisecond)
	}
	return false
}

// teardown stops whatever is left, waits for labd to be idle and takes the leak sample.
func (r *runner) teardown(ctx context.Context) {
	ctx = context.WithoutCancel(ctx)
	if rows, err := r.sessions(ctx); err == nil {
		for _, row := range rows {
			_ = r.api(ctx, http.MethodDelete, "/internal/sessions/"+row.SessionID, strings.NewReader(`{"reason":"admin_kill"}`), nil)
		}
	}
	if !r.waitIdle(ctx) {
		r.mark("labd still has sessions 60 s after teardown")
	}
	_ = sleepCtx(ctx, 5*time.Second) // goroutines of closed sockets unwind
	s := r.col.Sample(ctx, time.Now(), true)
	r.mu.Lock()
	r.samples = append(r.samples, s)
	r.mu.Unlock()
	r.mark("teardown done")
}

func (r *runner) write(rf RunFile) (string, error) {
	name := fmt.Sprintf("run-%s-%s-%s", r.cfg.Scenario, time.Now().UTC().Format("2006-01-02"), r.cfg.Host)
	if r.cfg.Label != "" {
		name += "-" + r.cfg.Label
	}
	if err := os.MkdirAll(r.cfg.Out, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(r.cfg.Out, name+".json")
	b, err := json.MarshalIndent(rf, "", " ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, append(b, '\n'), 0o644)
}

func hostInfo() (arch, kernel, runsc string) {
	arch = runtime.GOARCH
	if out, err := exec.Command("uname", "-m").Output(); err == nil {
		arch = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("uname", "-r").Output(); err == nil {
		kernel = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("runsc", "--version").Output(); err == nil {
		runsc, _, _ = strings.Cut(strings.TrimSpace(string(out)), "\n")
	}
	return
}
