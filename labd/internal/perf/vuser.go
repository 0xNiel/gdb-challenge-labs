package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"gdblabs/labd/internal/term/client"
)

// VUserConfig is one simulated learner.
type VUserConfig struct {
	API, WS, Secret string // labd's internal API, its WebSocket base (ws://host:port), the bearer secret
	UserID          int64
	Challenge       string
	Profile         Profile
	Hold            time.Duration // how long to run the profile once gdb is up
	QueueWait       time.Duration // longest wait for the lab to be running (default 3 min)
	Seed            uint64
	// Reconnect: when the socket drops (labd killed, P5), fetch a fresh token and reattach
	// within this long; 0 ends the user instead.
	Reconnect time.Duration
	Keep      bool // leave the session running at the end (no quit, no DELETE)

	// Clock hooks; the defaults are the real clock. Tests pass a virtual clock whose Sleep
	// returns at once, so a 30 s hold takes milliseconds.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
	HTTP  *http.Client
}

// VUserResult is what one user measured. Raw latency lists are pooled by the scenario and
// are not written per user.
type VUserResult struct {
	UserID          int64   `json:"user_id"`
	Profile         string  `json:"profile"`
	SessionID       string  `json:"session_id"`
	StartState      string  `json:"start_state"`    // state returned by POST: creating or queued
	QueuePosition   int     `json:"queue_position"` // position returned by POST (0: not queued)
	CreateHTTPMS    float64 `json:"create_http_ms"` // POST /internal/sessions response time
	QueueWaitMS     float64 `json:"queue_wait_ms"`  // POST to state running
	StartToPromptMS float64 `json:"start_to_prompt_ms"`
	Commands        int     `json:"commands"`
	Errors          int     `json:"errors"` // commands whose prompt did not come back within 60 s
	Echo            Pctl    `json:"echo_ms"`
	BytesIn         int64   `json:"ws_bytes_in"`  // keystrokes, client to labd
	BytesOut        int64   `json:"ws_bytes_out"` // terminal output, labd to client
	HoldS           float64 `json:"hold_s"`
	Ended           string  `json:"ended,omitempty"` // the lab ended during the run: its reason
	Err             string  `json:"err,omitempty"`
	Abuse           []Abuse `json:"abuse,omitempty"`
	Reconnects      int     `json:"reconnects,omitempty"`
	ReconnectMS     float64 `json:"reconnect_ms,omitempty"` // drop noticed to gdb answering again

	EchoMS []float64            `json:"-"` // every command's echo latency
	CmdMS  map[string][]float64 `json:"-"` // time to gdb's prompt, by command verb
}

// Abuse is one abuse action's outcome.
type Abuse struct {
	Action string  `json:"action"`
	MS     float64 `json:"ms"`
	Output string  `json:"output,omitempty"` // the probe's report line, or what bounded the action
	Warned bool    `json:"warned,omitempty"` // paste: the gateway sent a warn frame
}

var (
	gdbPrompt   = regexp.MustCompile(`\(gdb\) `)
	shellPrompt = regexp.MustCompile(`\$ `)
	probeLine   = regexp.MustCompile(`probe \w+ \w+=[^\r\n]*`)
)

// cmdTimeout bounds one command; a software watchpoint under gVisor can take seconds.
const cmdTimeout = 60 * time.Second

type vuser struct {
	cfg VUserConfig
	res VUserResult
	c   *client.Client
	t0  time.Time
}

// RunVUser runs one user from POST /internal/sessions to DELETE and reports what it
// measured. Cancelling ctx ends the hold early (churn) but still quits and deletes.
func RunVUser(ctx context.Context, cfg VUserConfig) VUserResult {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.QueueWait == 0 {
		cfg.QueueWait = 3 * time.Minute
	}
	v := &vuser{cfg: cfg, res: VUserResult{UserID: cfg.UserID, Profile: cfg.Profile.Name, CmdMS: map[string][]float64{}}}
	if err := v.run(ctx); err != nil {
		v.res.Err = err.Error()
	}
	if v.c != nil {
		for _, d := range v.c.Latencies() {
			v.res.EchoMS = append(v.res.EchoMS, ms(d))
		}
		v.res.Echo = Percentiles(v.res.EchoMS)
		in, out := v.c.Bytes()
		v.res.BytesOut += in // received by the client: terminal output
		v.res.BytesIn += out // sent by the client: keystrokes
		_ = v.c.Close()
	}
	if v.res.SessionID != "" && !cfg.Keep {
		v.delete()
	}
	return v.res
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func ms(d time.Duration) float64 { return round(float64(d.Microseconds())/1000, 3) }

type startResp struct {
	SessionID     string `json:"session_id"`
	WSToken       string `json:"ws_token"`
	State         string `json:"state"`
	QueuePosition int    `json:"queue_position"`
}

// start is POST /internal/sessions. A second call for a live session returns the same
// session with a fresh token (term README).
func (v *vuser) start(ctx context.Context) (startResp, error) {
	body, _ := json.Marshal(map[string]any{"user_id": v.cfg.UserID, "challenge_slug": v.cfg.Challenge})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, v.cfg.API+"/internal/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+v.cfg.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.cfg.HTTP.Do(req)
	if err != nil {
		return startResp{}, err
	}
	defer resp.Body.Close()
	var sr startResp
	if resp.StatusCode != http.StatusOK {
		var e map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return sr, fmt.Errorf("start: HTTP %d %v", resp.StatusCode, e)
	}
	return sr, json.NewDecoder(resp.Body).Decode(&sr)
}

func (v *vuser) delete() {
	req, _ := http.NewRequest(http.MethodDelete, v.cfg.API+"/internal/sessions/"+v.res.SessionID, strings.NewReader(`{"reason":"user_stop"}`))
	req.Header.Set("Authorization", "Bearer "+v.cfg.Secret)
	if resp, err := v.cfg.HTTP.Do(req); err == nil {
		resp.Body.Close()
	}
}

// attach dials the socket and waits until the lab is running.
func (v *vuser) attach(ctx context.Context, token string) error {
	c, err := client.Dial(ctx, v.cfg.WS+"/ws/term/"+v.res.SessionID, token, client.Options{})
	if err != nil {
		return err
	}
	v.c = c
	for {
		st, err := c.WaitControl("state", v.cfg.QueueWait)
		if err != nil {
			return fmt.Errorf("waiting for the lab: %w", err)
		}
		switch st["state"] {
		case "running":
			_ = c.Resize(160, 48)
			return nil
		case "ended":
			v.res.Ended, _ = st["reason"].(string)
			return fmt.Errorf("lab ended before running: %v", st["reason"])
		}
	}
}

func (v *vuser) run(hold context.Context) error {
	// Cancelling hold ends the hold only; getting in and out always completes.
	ctx := context.WithoutCancel(hold)
	v.t0 = time.Now()
	sr, err := v.start(ctx)
	v.res.CreateHTTPMS = ms(time.Since(v.t0))
	if err != nil {
		return err
	}
	v.res.SessionID, v.res.StartState, v.res.QueuePosition = sr.SessionID, sr.State, sr.QueuePosition
	if err := v.attach(ctx, sr.WSToken); err != nil {
		return err
	}
	v.res.QueueWaitMS = ms(time.Since(v.t0))
	if err := v.c.Send("gdb -q /opt/perf/perf"); err != nil {
		return err
	}
	if _, err := v.c.Expect(gdbPrompt, cmdTimeout); err != nil {
		return fmt.Errorf("gdb prompt: %w", err)
	}
	v.res.StartToPromptMS = ms(time.Since(v.t0))
	for _, cmd := range v.cfg.Profile.Setup {
		if err := v.cmd(ctx, cmd); err != nil {
			return err
		}
	}

	began := v.cfg.Now()
	end := began.Add(v.cfg.Hold)
	defer func() { v.res.HoldS = round(v.cfg.Now().Sub(began).Seconds(), 1) }()
	if v.cfg.Profile.Abuse {
		if err := v.abuse(hold); err != nil {
			return err
		}
		return v.idle(hold, end)
	}
	pc := NewPacer(v.cfg.Profile, began, v.cfg.Seed)
	for i := 0; ; i++ {
		due := pc.Next()
		if !due.Before(end) {
			break
		}
		if v.cfg.Sleep(hold, due.Sub(v.cfg.Now())) != nil {
			break // churn: this user's time is up
		}
		if err := v.cmd(ctx, v.cfg.Profile.Loop[i%len(v.cfg.Profile.Loop)]); err != nil {
			return err
		}
	}
	return v.quit()
}

func (v *vuser) quit() error {
	if v.cfg.Keep {
		return nil
	}
	if err := v.c.Send("quit"); err != nil {
		return nil // the lab is going away anyway
	}
	_, _ = v.c.Expect(shellPrompt, 10*time.Second)
	return nil
}

// cmd sends one gdb command and waits for the prompt. A socket that drops is reattached
// when Reconnect allows it, and the command is not retried.
func (v *vuser) cmd(ctx context.Context, cmd string) error {
	t := time.Now()
	if err := v.c.Send(cmd); err == nil {
		_, err = v.c.Expect(gdbPrompt, cmdTimeout)
		if err == nil {
			v.res.Commands++
			verb, _, _ := strings.Cut(cmd, " ")
			v.res.CmdMS[verb] = append(v.res.CmdMS[verb], ms(time.Since(t)))
			return nil
		}
		if !errors.Is(err, client.ErrClosed) {
			v.res.Commands++
			v.res.Errors++
			return nil
		}
	}
	return v.reconnect(ctx)
}

// reconnect handles a dropped socket: the lab ended (stop), or labd went away (P5).
func (v *vuser) reconnect(ctx context.Context) error {
	if st, err := v.c.WaitControl("state", 0); err == nil && st["state"] == "ended" {
		v.res.Ended, _ = st["reason"].(string)
		return fmt.Errorf("lab ended: %v", st["reason"])
	}
	if v.cfg.Reconnect == 0 {
		return fmt.Errorf("socket closed (code %d)", v.c.CloseStatus())
	}
	t := time.Now()
	deadline := t.Add(v.cfg.Reconnect)
	old := v.c
	for {
		sr, err := v.start(ctx)
		if err == nil {
			if sr.SessionID != v.res.SessionID {
				v.res.SessionID = sr.SessionID
				return fmt.Errorf("reconnect got a new session: the old one was not adopted")
			}
			in, out := old.Bytes()
			if err = v.attach(ctx, sr.WSToken); err == nil {
				// Carry the old socket's counters over; its latencies are pooled at the end.
				v.res.EchoMS = append(v.res.EchoMS, latMS(old)...)
				v.res.BytesOut += in
				v.res.BytesIn += out
				_ = v.c.Discard() // the scrollback replay
				if err = v.c.Send("print 1"); err == nil {
					_, err = v.c.Expect(gdbPrompt, cmdTimeout)
				}
				if err == nil {
					v.res.Reconnects++
					v.res.ReconnectMS = ms(time.Since(t))
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no reconnect within %s: %v", v.cfg.Reconnect, err)
		}
		if sleepCtx(ctx, 500*time.Millisecond) != nil {
			return ctx.Err()
		}
	}
}

func latMS(c *client.Client) []float64 {
	var out []float64
	for _, d := range c.Latencies() {
		out = append(out, ms(d))
	}
	return out
}

// Abuse actions (spec "abuser"; plan task 4.5). Each must be bounded by the sandbox (S7:
// CPU quota, process limit, tmpfs size) or the gateway (S13: input rate) without touching
// other labs. /opt/perf/probe does the fork and fill with raw syscalls, so the result does
// not depend on which shell tools labbase kept.
var abuseActions = []struct{ name, cmd string }{
	{"cpu_loop", `shell sh -c 'while :; do :; done' &`},
	{"fork", `shell /opt/perf/probe fork`},
	{"write_20mb", `shell /opt/perf/probe fill`},
}

// pasteBytes is the abuser's paste: gdb comment lines, so what gets through is harmless.
const pasteBytes = 100 << 10

func (v *vuser) abuse(ctx context.Context) error {
	for _, a := range abuseActions {
		t := time.Now()
		if err := v.c.Send(a.cmd); err != nil {
			return err
		}
		out, err := v.c.Expect(gdbPrompt, cmdTimeout)
		if err != nil {
			return fmt.Errorf("%s: %w", a.name, err)
		}
		v.res.Abuse = append(v.res.Abuse, Abuse{Action: a.name, MS: ms(time.Since(t)), Output: probeLine.FindString(out)})
		v.res.Commands++
	}
	// The paste goes out as fast as the socket takes it, in 16 KiB messages (the gateway
	// caps one message at 64 KiB). Everything past the 16 KiB burst is dropped with a warn.
	line := "#" + strings.Repeat("x", 98) + "\n"
	paste := []byte(strings.Repeat(line, pasteBytes/len(line)))
	t := time.Now()
	for p := paste; len(p) > 0; {
		n := min(len(p), 16<<10)
		if err := v.c.SendRaw(p[:n]); err != nil {
			return err
		}
		p = p[n:]
	}
	_, werr := v.c.WaitControl("warn", 10*time.Second)
	// Let the input bucket refill, then clear the half line the limit left behind.
	if err := v.cfg.Sleep(ctx, 10*time.Second); err != nil {
		return nil
	}
	_ = v.c.SendRaw([]byte{0x03})
	_ = v.cfg.Sleep(ctx, 2*time.Second)
	_ = v.c.Discard()
	v.res.Abuse = append(v.res.Abuse, Abuse{Action: "paste_100kb", MS: ms(time.Since(t)), Warned: werr == nil})
	return v.cmd(ctx, "print 1")
}

// idle keeps the abuser's lab alive until end: one cheap command a minute resets the idle
// timer and measures echo latency while its CPU loop burns its quota.
func (v *vuser) idle(ctx context.Context, end time.Time) error {
	for {
		next := v.cfg.Now().Add(time.Minute)
		if !next.Before(end) {
			return v.quit()
		}
		if v.cfg.Sleep(ctx, time.Minute) != nil {
			return v.quit()
		}
		if err := v.cmd(ctx, "print 1"); err != nil {
			return err
		}
	}
}
