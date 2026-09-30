package term

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/config"
	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
)

const origin = "https://labs.example.com"

// ---------------------------------------------------------------- fake runtime

// ptyRuntime is an orch.Runtime whose containers echo keystrokes like a cooked terminal
// and let tests inject output.
type ptyRuntime struct {
	mu   sync.Mutex
	ctrs map[string]*ptyCtr
}

func (r *ptyRuntime) Create(_ context.Context, o orch.CreateOpts) (orch.Container, error) {
	c := &ptyCtr{id: o.ID, done: make(chan struct{})}
	c.inR, c.inW = io.Pipe()
	c.outR, c.outW = io.Pipe()
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := c.inR.Read(buf)
			if n > 0 {
				c.mu.Lock()
				c.stdin.Write(buf[:n])
				c.mu.Unlock()
				_, _ = c.outW.Write(buf[:n]) // echo
			}
			if err != nil {
				return
			}
		}
	}()
	r.mu.Lock()
	r.ctrs[o.ID] = c
	r.mu.Unlock()
	return c, nil
}
func (r *ptyRuntime) List(context.Context) ([]orch.ContainerInfo, error) { return nil, nil }
func (r *ptyRuntime) Attach(context.Context, string) (orch.Container, error) {
	return nil, errors.New("not supported")
}
func (r *ptyRuntime) Remove(context.Context, string) error { return nil }
func (r *ptyRuntime) get(sessionID string) *ptyCtr {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctrs["lab-"+sessionID]
}

type ptyCtr struct {
	id         string
	inR, outR  *io.PipeReader
	inW, outW  *io.PipeWriter
	done       chan struct{}
	once       sync.Once
	mu         sync.Mutex
	stdin      bytes.Buffer
	cols, rows uint32
	emitted    atomic.Int64
}

func (c *ptyCtr) ID() string                      { return c.id }
func (c *ptyCtr) Start(context.Context) error     { return nil }
func (c *ptyCtr) Kill(context.Context) error      { c.stop(); return nil }
func (c *ptyCtr) Delete(context.Context) error    { c.stop(); return nil }
func (c *ptyCtr) Done() <-chan struct{}           { return c.done }
func (c *ptyCtr) IO() (io.WriteCloser, io.Reader) { return c.inW, c.outR }
func (c *ptyCtr) CgroupPath() string              { return "" }
func (c *ptyCtr) Resize(_ context.Context, cols, rows uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cols, c.rows = cols, rows
	return nil
}
func (c *ptyCtr) stop() {
	c.once.Do(func() { close(c.done); c.inW.Close(); c.outW.Close() })
}
func (c *ptyCtr) emit(p []byte) {
	if _, err := c.outW.Write(p); err == nil {
		c.emitted.Add(int64(len(p)))
	}
}
func (c *ptyCtr) input() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stdin.String()
}

// ---------------------------------------------------------------- harness

type harness struct {
	clk    clock.Clock
	fake   *clock.Fake // nil when the harness runs on the real clock
	st     *store.Memory
	rt     *ptyRuntime
	m      *orch.Manager
	tokens *Tokens
	srv    *Server
	ts     *httptest.Server
}

func newHarness(t *testing.T, maxSessions int, real bool, tune func(*Options)) *harness {
	t.Helper()
	base, err := os.ReadFile("../../sandbox/sandbox-base.json")
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{st: store.NewMemory(), rt: &ptyRuntime{ctrs: map[string]*ptyCtr{}}, tokens: NewTokens([]byte("k"))}
	if real {
		h.clk = clock.Real{}
	} else {
		h.fake = clock.NewFake(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC))
		h.clk = h.fake
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h.m = orch.NewManager(orch.ManagerConfig{BaseSpec: base, MaxSessions: maxSessions, MaxQueue: 5,
		DefaultLimits: config.Default().DefaultLimits,
		Challenges:    []orch.Challenge{{Slug: "perf", Image: "img", Enabled: true}}}, h.rt, h.st, h.clk, log)
	rec := NewRecorder(h.st, h.clk, log)
	o := Options{Sessions: h.m, Tokens: h.tokens, Recorder: rec, Clock: h.clk, Log: log, SiteOrigin: origin}
	if tune != nil {
		tune(&o)
	}
	h.srv = New(o)
	h.ts = httptest.NewServer(h.srv.Handler())
	t.Cleanup(func() { h.ts.Close(); h.m.Close(); rec.Close() })
	return h
}

func (h *harness) start(t *testing.T, user int64) string {
	t.Helper()
	in, err := h.m.Start(context.Background(), orch.StartReq{UserID: user, ChallengeSlug: "perf"})
	if err != nil {
		t.Fatal(err)
	}
	h.m.Settle()
	return in.ID
}

func (h *harness) url(id string) string {
	return "ws" + strings.TrimPrefix(h.ts.URL, "http") + "/ws/term/" + id
}

func (h *harness) dialAs(t *testing.T, id string, user int64, org string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	tok := h.tokens.Mint(id, user, h.clk.Now())
	hdr := http.Header{}
	if org != "" {
		hdr.Set("Origin", org)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, h.url(id)+"?t="+tok, &websocket.DialOptions{HTTPHeader: hdr})
}

func (h *harness) dial(t *testing.T, id string, user int64) *wsClient {
	t.Helper()
	ws, _, err := h.dialAs(t, id, user, origin)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	ws.SetReadLimit(4 << 20)
	c := &wsClient{ws: ws, frames: make(chan frame, 1024)}
	go c.read()
	t.Cleanup(func() { ws.CloseNow() })
	return c
}

// wsClient reads frames in the background so tests can wait for specific ones.
type wsClient struct {
	ws     *websocket.Conn
	frames chan frame
	err    atomic.Value
	out    bytes.Buffer // binary output seen so far (only touched by the test goroutine)
}

type frame struct {
	text map[string]any
	bin  []byte
}

func (c *wsClient) read() {
	for {
		typ, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.err.Store(err)
			close(c.frames)
			return
		}
		if typ == websocket.MessageText {
			var m map[string]any
			_ = json.Unmarshal(data, &m)
			c.frames <- frame{text: m}
		} else {
			c.frames <- frame{bin: data}
		}
	}
}

// expect returns the next control frame of type typ, collecting binary output on the way.
func (c *wsClient) expect(t *testing.T, typ string) map[string]any {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case f, ok := <-c.frames:
			if !ok {
				t.Fatalf("connection closed waiting for %q: %v", typ, c.err.Load())
			}
			if f.bin != nil {
				c.out.Write(f.bin)
			} else if f.text["type"] == typ {
				return f.text
			}
		case <-timeout:
			t.Fatalf("no %q frame within 5 s", typ)
		}
	}
}

// expectOutput waits until the binary output contains s.
func (c *wsClient) expectOutput(t *testing.T, s string) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for !strings.Contains(c.out.String(), s) {
		select {
		case f, ok := <-c.frames:
			if !ok {
				t.Fatalf("connection closed waiting for %q: %v", s, c.err.Load())
			}
			c.out.Write(f.bin)
		case <-timeout:
			t.Fatalf("output %q never contained %q", c.out.String(), s)
		}
	}
}

// closed waits for the connection to end and returns its close status.
func (c *wsClient) closed(t *testing.T) (websocket.StatusCode, string) {
	t.Helper()
	timeout := time.After(15 * time.Second)
	for {
		select {
		case _, ok := <-c.frames:
			if !ok {
				err, _ := c.err.Load().(error)
				var ce websocket.CloseError
				if errors.As(err, &ce) {
					return ce.Code, ce.Reason
				}
				return -1, fmt.Sprint(err)
			}
		case <-timeout:
			t.Fatal("connection not closed within 15 s")
		}
	}
}

func (c *wsClient) send(t *testing.T, p string) {
	t.Helper()
	if err := c.ws.Write(context.Background(), websocket.MessageBinary, []byte(p)); err != nil {
		t.Fatal(err)
	}
}

func (c *wsClient) ctrl(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if err := c.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- 3.2 handshake

func TestWS_Handshake(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 2, false, nil)
	id := h.start(t, 1)

	status := func(url string, org string) int {
		hdr := http.Header{}
		if org != "" {
			hdr.Set("Origin", org)
		}
		_, resp, err := websocket.Dial(context.Background(), url, &websocket.DialOptions{HTTPHeader: hdr})
		if err == nil {
			t.Fatalf("%s: dial succeeded", url)
		}
		if resp == nil {
			t.Fatalf("%s: no HTTP response: %v", url, err)
		}
		return resp.StatusCode
	}
	good := h.tokens.Mint(id, 1, h.clk.Now())
	if got := status(h.url(id)+"?t=garbage", origin); got != http.StatusUnauthorized {
		t.Errorf("bad token: %d, want 401", got)
	}
	if got := status(h.url(id)+"?t="+good, "https://evil.example"); got != http.StatusForbidden {
		t.Errorf("bad origin: %d, want 403", got)
	}
	if got := status(h.url(id)+"?t="+good, ""); got != http.StatusForbidden {
		t.Errorf("no origin (not allowed in this config): %d, want 403", got)
	}
	if got := status(h.url("nope")+"?t="+h.tokens.Mint("nope", 1, h.clk.Now()), origin); got != http.StatusNotFound {
		t.Errorf("unknown session: %d, want 404", got)
	}
	if got := status(h.url(id)+"?t="+h.tokens.Mint(id, 2, h.clk.Now()), origin); got != http.StatusUnauthorized {
		t.Errorf("another user's token: %d, want 401", got)
	}
	// The origin check ran first, so the good token is still unused.
	c := h.dial(t, id, 1)
	if st := c.expect(t, "state"); st["state"] != "running" {
		t.Fatalf("first frame %v", st)
	}
	if _, _, err := websocket.Dial(context.Background(), h.url(id)+"?t="+good, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": {origin}}}); err != nil {
		t.Fatalf("good token after a rejected origin: %v", err)
	}
}

func TestWS_AllowNoOriginForDev(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, func(o *Options) { o.AllowNoOrigin = true })
	id := h.start(t, 1)
	if _, _, err := h.dialAs(t, id, 1, ""); err != nil {
		t.Fatalf("dev: dial without Origin: %v", err)
	}
	if _, resp, err := h.dialAs(t, id, 1, "https://evil.example"); err == nil || resp.StatusCode != 403 {
		t.Fatal("dev mode must still reject a wrong Origin")
	}
}

// ---------------------------------------------------------------- 3.3 bridge

func TestWS_Bridge(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	pty := h.rt.get(id)
	pty.emit([]byte("$ ")) // printed before anyone connected: arrives as scrollback
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	c.expectOutput(t, "$ ")

	c.send(t, "hello\n")
	c.expectOutput(t, "hello\n")
	waitFor(t, "keystrokes on the PTY", func() bool { return pty.input() == "hello\n" })
	pty.emit([]byte("from the lab"))
	c.expectOutput(t, "from the lab")

	c.ctrl(t, map[string]any{"type": "resize", "cols": 120, "rows": 40})
	waitFor(t, "resize", func() bool { pty.mu.Lock(); defer pty.mu.Unlock(); return pty.cols == 120 && pty.rows == 40 })

	before, _ := h.m.Get(id)
	h.fake.Advance(time.Minute)
	c.ctrl(t, map[string]any{"type": "ping"})
	c.ctrl(t, map[string]any{"type": "nonsense"})
	waitFor(t, "unknown frame counted", func() bool { return h.srv.UnknownFrames() == 1 })
	if after, _ := h.m.Get(id); !after.IdleDeadline.Equal(before.IdleDeadline) {
		t.Fatal("ping reset the idle timer")
	}
	c.send(t, "x")
	waitFor(t, "input resets idle", func() bool {
		in, _ := h.m.Get(id)
		return in.IdleDeadline.After(before.IdleDeadline)
	})
}

func TestWS_SecondConnectionReplacesFirst(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	first := h.dial(t, id, 1)
	first.expect(t, "state")
	second := h.dial(t, id, 1)
	second.expect(t, "state")
	if code, reason := first.closed(t); code != websocket.StatusNormalClosure || reason != "replaced" {
		t.Fatalf("first closed with %d %q, want 1000 replaced", code, reason)
	}
	second.send(t, "still mine\n")
	second.expectOutput(t, "still mine")
	if in, _ := h.m.Get(id); !in.Attached || in.State != orch.StateRunning {
		t.Fatalf("session after replacement: %+v", in)
	}
}

func TestWS_QueuedThenRunning(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	first := h.start(t, 1)
	id := h.start(t, 2)
	c := h.dial(t, id, 2)
	if q := c.expect(t, "queued"); q["position"] != float64(1) {
		t.Fatalf("queued frame %v", q)
	}
	h.m.Stop(context.Background(), first, orch.ReasonUserStop)
	h.m.Settle()
	h.fake.Advance(time.Second) // queue poll
	if st := c.expect(t, "state"); st["state"] != "running" {
		t.Fatalf("state %v", st)
	}
}

func TestWS_EndedFrameAndClose(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	h.m.Stop(context.Background(), id, orch.ReasonAdminKill)
	st := c.expect(t, "state")
	if st["state"] != "ended" || st["reason"] != orch.ReasonAdminKill {
		t.Fatalf("state frame %v", st)
	}
	if code, reason := c.closed(t); code != websocket.StatusNormalClosure || reason != "ended" {
		t.Fatalf("closed with %d %q", code, reason)
	}
}

// ---------------------------------------------------------------- 3.4 rate limits

func TestWS_InputBurstLimited(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	c.send(t, strings.Repeat("a", 20<<10))
	c.expect(t, "warn")
	time.Sleep(100 * time.Millisecond)
	if n := len(h.rt.get(id).input()); n > InputBurst || n < InputBurst-64 {
		t.Fatalf("%d bytes reached the PTY from a 20 KiB burst, want about %d", n, InputBurst)
	}
	c.send(t, strings.Repeat("b", 100)) // still over: dropped, but no second warn within a second
	time.Sleep(100 * time.Millisecond)
	select {
	case f := <-c.frames:
		if f.text != nil && f.text["type"] == "warn" {
			t.Fatal("second warn within one second")
		}
	default:
	}
}

func TestWS_OutputRateLimited(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, true, nil) // real clock: this measures a real second
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	go h.rt.get(id).emit(bytes.Repeat([]byte("x"), 1<<20))
	var got int
	var t1 time.Time
	deadline := time.After(3 * time.Second)
	for {
		select {
		case f := <-c.frames:
			if f.bin == nil {
				continue
			}
			if t1.IsZero() {
				t1 = time.Now()
			}
			if time.Since(t1) <= time.Second {
				got += len(f.bin)
			}
		case <-deadline:
			lo, hi := OutputRate*8/10, OutputRate*12/10
			if got < lo || got > hi {
				t.Fatalf("%d bytes in the first second, want %d..%d", got, lo, hi)
			}
			return
		}
	}
}

func TestWS_SlowConsumerClosed1008(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, func(o *Options) { o.OutputRate, o.OutputBurst = 1<<30, 32<<10 })
	id := h.start(t, 1)
	ws, _, err := h.dialAs(t, id, 1, origin)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(4 << 20)
	pty := h.rt.get(id)
	// The client reads nothing. Keep printing until the gateway's buffer backs up and the
	// lab's output blocks.
	go func() {
		chunk := bytes.Repeat([]byte("y"), 32<<10)
		for i := 0; i < 16384; i++ {
			pty.emit(chunk)
		}
	}()
	var last int64 = -1
	waitFor(t, "output to stall", func() bool {
		n := pty.emitted.Load()
		stalled := n == last && n > 0
		last = n
		time.Sleep(50 * time.Millisecond)
		return stalled
	})
	// Advance until the watchdog has decided (the session shows no client attached).
	deadline := time.Now().Add(30 * time.Second)
	for {
		if in, _ := h.m.Get(id); !in.Attached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("slow consumer never detected")
		}
		h.fake.Advance(time.Second)
		time.Sleep(20 * time.Millisecond)
	}
	// Resume reading: the backlog drains and the close frame follows it.
	var code websocket.StatusCode
	for {
		_, _, err := ws.Read(context.Background())
		if err != nil {
			code = websocket.CloseStatus(err)
			break
		}
	}
	if code != websocket.StatusPolicyViolation {
		t.Fatalf("close status %d, want 1008", code)
	}
	if in, _ := h.m.Get(id); in.State != orch.StateRunning {
		t.Fatalf("session %s after a slow consumer; it must survive for the grace window", in.State)
	}
}

// ---------------------------------------------------------------- 3.6 grace, TTL, extend

func TestWS_GraceAfterClose(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	c.ws.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "detach", func() bool { in, _ := h.m.Get(id); return !in.Attached })
	h.fake.Advance(59 * time.Second)
	if in, _ := h.m.Get(id); in.State != orch.StateRunning {
		t.Fatalf("ended inside the grace: %s", in.State)
	}
	h.fake.Advance(2 * time.Second)
	h.m.Settle()
	if row, _ := h.st.Session(id); row.State != "ended" || row.EndReason != orch.ReasonWSClosed {
		t.Fatalf("row %s/%s, want ended/ws_closed", row.State, row.EndReason)
	}
}

func TestWS_ReconnectWithinGrace(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	h.rt.get(id).emit([]byte("before the drop"))
	c.expectOutput(t, "before the drop")
	c.ws.Close(websocket.StatusNormalClosure, "")
	waitFor(t, "detach", func() bool { in, _ := h.m.Get(id); return !in.Attached })
	h.fake.Advance(30 * time.Second)
	again := h.dial(t, id, 1)
	again.expect(t, "state")
	again.expectOutput(t, "before the drop") // scrollback replayed
	h.fake.Advance(2 * time.Minute)
	if in, _ := h.m.Get(id); in.State != orch.StateRunning {
		t.Fatalf("reconnected session is %s", in.State)
	}
}

func TestWS_TTLFramesAndExtend(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	first := c.expect(t, "ttl")
	if first["idle_remaining_s"] != float64(900) || first["hard_remaining_s"] != float64(3600) || first["extend_available"] != true {
		t.Fatalf("first ttl %v", first)
	}
	h.fake.Advance(30 * time.Second)
	second := c.expect(t, "ttl")
	if second["idle_remaining_s"] != float64(870) || second["hard_remaining_s"] != float64(3570) {
		t.Fatalf("second ttl %v", second)
	}
	c.ctrl(t, map[string]any{"type": "extend"})
	if e := c.expect(t, "extend"); e["ok"] != true {
		t.Fatalf("first extend %v", e)
	}
	c.ctrl(t, map[string]any{"type": "extend"})
	if e := c.expect(t, "extend"); e["ok"] != false {
		t.Fatalf("second extend %v", e)
	}
	h.fake.Advance(30 * time.Second)
	third := c.expect(t, "ttl")
	if third["idle_remaining_s"] != float64(1770) || third["extend_available"] != false {
		t.Fatalf("ttl after extend %v; want 1770 (a 30 min idle window from the extend, 30 s ago)", third)
	}
}

// ---------------------------------------------------------------- 3.5 capture over the socket

func TestWS_CaptureReachesStore(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c := h.dial(t, id, 1)
	c.expect(t, "state")
	c.send(t, "break main\r")
	c.send(t, "\x1b[Anexx\x7ft\r\x03")
	c.expectOutput(t, "\x03")
	waitFor(t, "3 commands counted", func() bool { in, _ := h.m.Get(id); return in.Commands == 3 })
	h.fake.Advance(time.Second) // recorder flush
	var lines []string
	waitFor(t, "3 command_entered events", func() bool {
		lines = nil
		for _, e := range h.st.Events() {
			if e.Type == "command_entered" {
				if e.Data["seq"] != len(lines)+1 {
					return false
				}
				lines = append(lines, e.Data["line"].(string))
			}
		}
		return len(lines) == 3
	})
	if strings.Join(lines, "|") != "break main|next|^C" {
		t.Fatalf("captured %q", lines)
	}
}

// A client that never reads again must not keep the connection's goroutines alive.
func TestWS_StuckClientIsReleased(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, func(o *Options) { o.OutputRate, o.OutputBurst = 1<<30, 32<<10 })
	id := h.start(t, 1)
	ws, _, err := h.dialAs(t, id, 1, origin)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow()
	pty := h.rt.get(id)
	go func() {
		chunk := bytes.Repeat([]byte("z"), 32<<10)
		for i := 0; i < 16384; i++ {
			pty.emit(chunk)
		}
	}()
	var last int64 = -1
	waitFor(t, "output to stall", func() bool {
		n := pty.emitted.Load()
		stalled := n == last && n > 0
		last = n
		time.Sleep(50 * time.Millisecond)
		return stalled
	})
	// Never read. Advance the clock until the watchdog gives up on the client; the gateway
	// must then release it within flushWait plus the close timeout.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if in, _ := h.m.Get(id); !in.Attached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection to a stuck client never released")
		}
		h.fake.Advance(time.Second)
		time.Sleep(20 * time.Millisecond)
	}
	// The lab's output is flowing again (nobody is subscribed).
	before := pty.emitted.Load()
	waitFor(t, "output to resume", func() bool { return pty.emitted.Load() > before })
}
