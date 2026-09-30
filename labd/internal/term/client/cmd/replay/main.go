// Command replay drives one lab like a person working in gdb (Phase 3, task 3.10, P1).
//
//	go run ./internal/term/client/cmd/replay -script ../images/perf/session.gdb -duration 10m
//
// It starts a session through the internal API, waits for it to run, starts gdb, then sends
// the script's commands one by one at -rate per minute, looping the script until -duration
// has passed. Each command waits for gdb's prompt before the next. It writes a JSON summary
// (start to prompt, echo latency per command, bytes each way) to -out.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"gdblabs/labd/internal/term/client"
)

type summary struct {
	SessionID       string `json:"session_id"`
	StartToPromptMS int64  `json:"start_to_prompt_ms"`
	DurationS       int    `json:"duration_s"`
	Commands        int    `json:"commands"`
	Errors          int    `json:"errors"`
	EchoMS          pctls  `json:"echo_latency_ms"`
	WSBytesIn       int64  `json:"ws_bytes_in"`  // client -> server (keystrokes)
	WSBytesOut      int64  `json:"ws_bytes_out"` // server -> client (terminal output)
}

type pctls struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
}

func main() {
	api := flag.String("api", "http://127.0.0.1:18081", "labd internal API")
	wsBase := flag.String("ws", "ws://127.0.0.1:18082", "labd WebSocket gateway")
	user := flag.Int64("user", 4000001, "user id to start the lab as")
	slug := flag.String("challenge", "perf", "challenge slug")
	script := flag.String("script", "images/perf/session.gdb", "gdb command list (session.gdb)")
	rate := flag.Float64("rate", 20, "commands per minute")
	duration := flag.Duration("duration", 10*time.Minute, "how long to keep replaying")
	out := flag.String("out", "", "write the JSON summary here (default stdout)")
	sessionFile := flag.String("session-file", "", "write the session id here as soon as it is known")
	flag.Parse()
	if err := run(*api, *wsBase, *user, *slug, *script, *rate, *duration, *out, *sessionFile); err != nil {
		fmt.Fprintln(os.Stderr, "replay:", err)
		os.Exit(1)
	}
}

func run(api, wsBase string, user int64, slug, script string, rate float64, duration time.Duration, out, sessionFile string) error {
	cmds, err := readScript(script)
	if err != nil {
		return err
	}
	secret := os.Getenv("LABD_INTERNAL_SECRET")
	ctx := context.Background()

	t0 := time.Now()
	body, _ := json.Marshal(map[string]any{"user_id": user, "challenge_slug": slug})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, api+"/internal/sessions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	var start struct {
		SessionID string `json:"session_id"`
		WSToken   string `json:"ws_token"`
	}
	err = json.NewDecoder(resp.Body).Decode(&start)
	resp.Body.Close()
	if err != nil || resp.StatusCode != http.StatusOK {
		return fmt.Errorf("start session: HTTP %d, %v", resp.StatusCode, err)
	}
	if sessionFile != "" {
		_ = os.WriteFile(sessionFile, []byte(start.SessionID+"\n"), 0o644)
	}
	defer stop(api, secret, start.SessionID)

	c, err := client.Dial(ctx, wsBase+"/ws/term/"+start.SessionID, start.WSToken, client.Options{})
	if err != nil {
		return err
	}
	defer c.Close()
	for {
		st, err := c.WaitControl("state", 2*time.Minute)
		if err != nil {
			return fmt.Errorf("waiting for the lab: %w", err)
		}
		if st["state"] == "running" {
			break
		}
		if st["state"] == "ended" {
			return fmt.Errorf("lab ended before running: %v", st["reason"])
		}
	}
	_ = c.Resize(160, 48)
	prompt := regexp.MustCompile(`\(gdb\) `)
	if err := c.Send("gdb -q /opt/perf/perf"); err != nil {
		return err
	}
	if _, err := c.Expect(prompt, 60*time.Second); err != nil {
		return fmt.Errorf("gdb prompt: %w", err)
	}
	s := summary{SessionID: start.SessionID, StartToPromptMS: time.Since(t0).Milliseconds()}
	fmt.Fprintf(os.Stderr, "replay: session %s, gdb prompt after %d ms\n", s.SessionID, s.StartToPromptMS)

	gap := time.Duration(float64(time.Minute) / rate)
	began := time.Now()
	next := began
	for i := 0; time.Since(began) < duration; i++ {
		time.Sleep(time.Until(next))
		next = next.Add(gap)
		cmd := cmds[i%len(cmds)]
		if err := c.Send(cmd); err != nil {
			return fmt.Errorf("send %q: %w", cmd, err)
		}
		s.Commands++
		if _, err := c.Expect(prompt, 60*time.Second); err != nil {
			if strings.Contains(err.Error(), client.ErrClosed.Error()) {
				return err
			}
			s.Errors++
			fmt.Fprintf(os.Stderr, "replay: %q: %v\n", cmd, err)
		}
		if s.Commands%50 == 0 {
			fmt.Fprintf(os.Stderr, "replay: %d commands, %s\n", s.Commands, time.Since(began).Round(time.Second))
		}
	}
	s.DurationS = int(time.Since(began).Seconds())
	_ = c.Send("quit")

	lat := c.Latencies()[1:] // the first is the shell echoing "gdb ..."
	ms := make([]float64, len(lat))
	for i, d := range lat {
		ms[i] = float64(d.Microseconds()) / 1000
	}
	slices.Sort(ms)
	s.EchoMS = pctls{P50: pct(ms, 50), P95: pct(ms, 95), Max: pct(ms, 100)}
	s.WSBytesOut, s.WSBytesIn = c.Bytes()

	b, _ := json.MarshalIndent(s, "", "  ")
	if out == "" {
		fmt.Println(string(b))
		return nil
	}
	return os.WriteFile(out, append(b, '\n'), 0o644)
}

// readScript returns the gdb commands of session.gdb: no comments, no blank lines.
func readScript(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cmds []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l != "" && !strings.HasPrefix(l, "#") {
			cmds = append(cmds, l)
		}
	}
	if len(cmds) == 0 {
		return nil, fmt.Errorf("%s has no commands", path)
	}
	return cmds, sc.Err()
}

func stop(api, secret, id string) {
	req, _ := http.NewRequest(http.MethodDelete, api+"/internal/sessions/"+id, strings.NewReader(`{"reason":"user_stop"}`))
	req.Header.Set("Authorization", "Bearer "+secret)
	if resp, err := http.DefaultClient.Do(req); err == nil {
		resp.Body.Close()
	}
}

// pct is the nearest-rank percentile of sorted values (p=100 is the max).
func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p/100*float64(len(sorted))+0.999999) - 1
	i = max(0, min(i, len(sorted)-1))
	return float64(int(sorted[i]*1000)) / 1000
}
