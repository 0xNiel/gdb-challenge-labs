package perf

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeLabd speaks labd's internal API and WebSocket protocol, with a terminal that behaves
// like a shell running gdb: enough to drive a virtual user end to end without containers.
type fakeLabd struct {
	srv     *httptest.Server
	mu      sync.Mutex
	deleted []string
	lines   []string // every line the "terminal" received
}

func newFakeLabd(t *testing.T) *fakeLabd {
	t.Helper()
	f := &fakeLabd{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/sessions", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			UserID int64 `json:"user_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		_ = json.NewEncoder(w).Encode(map[string]any{"session_id": fmt.Sprintf("s%d", b.UserID), "ws_token": "tok", "state": "creating"})
	})
	mux.HandleFunc("DELETE /internal/sessions/{id}", func(_ http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.deleted = append(f.deleted, r.PathValue("id"))
		f.mu.Unlock()
	})
	mux.HandleFunc("GET /ws/term/{id}", f.term)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeLabd) term(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ctx := context.Background()
	out := func(s string) { _ = ws.Write(ctx, websocket.MessageBinary, []byte(s)) }
	_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"state","state":"running"}`))
	out("lab$ ")
	inGdb, pasted, warned := false, 0, false
	var line strings.Builder
	for {
		typ, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		if typ != websocket.MessageBinary {
			continue
		}
		if len(data) > 1024 { // a paste: the 16 KiB burst gets through, the rest is dropped
			allowed := max(0, min(len(data), 16<<10-pasted))
			pasted += len(data)
			data = data[:allowed]
			if pasted > 16<<10 && !warned {
				warned = true
				_ = ws.Write(ctx, websocket.MessageText, []byte(`{"type":"warn","message":"input rate limit"}`))
			}
		}
		out(string(data)) // echo
		for _, b := range data {
			switch b {
			case 0x03:
				line.Reset()
				out("Quit\r\n(gdb) ")
			case '\n':
				l := line.String()
				line.Reset()
				f.mu.Lock()
				f.lines = append(f.lines, l)
				f.mu.Unlock()
				switch {
				case !inGdb && strings.HasPrefix(l, "gdb "):
					inGdb = true
					out("(gdb) ")
				case inGdb && l == "quit":
					inGdb = false
					out("lab$ ")
				case strings.Contains(l, "probe fork"):
					out("probe fork forked=30 error=EAGAIN\r\n(gdb) ")
				case strings.Contains(l, "probe fill"):
					out("probe fill bytes=16777216 error=ENOSPC\r\n(gdb) ")
				case inGdb:
					out("(gdb) ")
				default:
					out("lab$ ")
				}
			default:
				line.WriteByte(b)
			}
		}
	}
}

func (f *fakeLabd) cfg(p Profile, user int64, hold time.Duration) VUserConfig {
	vc := &vclock{t: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}
	return VUserConfig{
		API: f.srv.URL, WS: "ws" + strings.TrimPrefix(f.srv.URL, "http"), Secret: "s",
		UserID: user, Challenge: "perf", Profile: p, Hold: hold, Seed: uint64(user),
		Now: vc.Now, Sleep: vc.Sleep,
	}
}

// vclock is a virtual clock: Sleep returns at once and moves Now forward.
type vclock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *vclock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *vclock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.t = c.t.Add(max(d, 0))
	c.mu.Unlock()
	return nil
}

func TestVUser_StepperRun(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	ps, _ := Profiles(loadRealScript(t))
	res := RunVUser(context.Background(), f.cfg(ps[Stepper], 7, 30*time.Second))
	if res.Err != "" {
		t.Fatalf("vuser failed: %s", res.Err)
	}
	loop := res.Commands - len(ps[Stepper].Setup)
	if loop < 8 || loop > 12 {
		t.Errorf("%d commands in a 30 s stepper hold, want about 10", loop)
	}
	if res.Errors != 0 || res.StartToPromptMS <= 0 || res.SessionID != "s7" {
		t.Errorf("result %+v", res)
	}
	if len(res.EchoMS) < res.Commands || res.Echo.N != len(res.EchoMS) || res.Echo.P95 <= 0 {
		t.Errorf("%d echo latencies for %d commands (%+v)", len(res.EchoMS), res.Commands, res.Echo)
	}
	if len(res.CmdMS["set"]) < 2 || len(res.CmdMS["set"])+len(res.CmdMS["continue"])+len(res.CmdMS["next"]) == 0 {
		t.Errorf("per-verb latencies %v", res.CmdMS)
	}
	if res.BytesIn == 0 || res.BytesOut == 0 {
		t.Errorf("bytes in %d out %d", res.BytesIn, res.BytesOut)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleted) != 1 || f.deleted[0] != "s7" {
		t.Errorf("deleted %v", f.deleted)
	}
	if f.lines[len(f.lines)-1] != "quit" {
		t.Errorf("last line %q, want quit", f.lines[len(f.lines)-1])
	}
}

func TestVUser_Abuser(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	ps, _ := Profiles(loadRealScript(t))
	res := RunVUser(context.Background(), f.cfg(ps[Abuser], 9, 3*time.Minute))
	if res.Err != "" {
		t.Fatalf("vuser failed: %s", res.Err)
	}
	got := map[string]Abuse{}
	for _, a := range res.Abuse {
		got[a.Action] = a
	}
	if len(got) != 4 || got["fork"].Output != "probe fork forked=30 error=EAGAIN" ||
		!strings.HasPrefix(got["write_20mb"].Output, "probe fill") || !got["paste_100kb"].Warned {
		t.Fatalf("abuse results %+v", res.Abuse)
	}
	// After the paste the terminal answers again, and the idle loop keeps the lab alive.
	if n := len(res.CmdMS["print"]); n < 3 {
		t.Errorf("%d print commands after the abuse, want one plus one a minute", n)
	}
	if res.Errors != 0 {
		t.Errorf("%d command errors", res.Errors)
	}
}

func TestVUser_CancelEndsHoldAndDeletes(t *testing.T) {
	t.Parallel()
	f := newFakeLabd(t)
	ps, _ := Profiles(loadRealScript(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := f.cfg(ps[Reader], 3, time.Hour)
	res := RunVUser(ctx, cfg)
	if res.Err != "" {
		t.Fatalf("vuser failed: %s", res.Err)
	}
	if res.HoldS != 0 {
		t.Errorf("hold %v s after cancel, want 0", res.HoldS)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.deleted) != 1 {
		t.Errorf("deleted %v", f.deleted)
	}
}
