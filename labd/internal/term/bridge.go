package term

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"

	"gdblabs/labd/internal/orch"
	"gdblabs/labd/internal/store"
)

// conn is one WebSocket bridged to a session's terminal. Three goroutines: run (lifecycle,
// TTL frames, slow-consumer watchdog), readLoop (keystrokes and control frames) and
// writeLoop (the only writer: control frames, scrollback, output).
//
// Reads and writes use a background context on purpose: in coder/websocket a cancelled
// context closes the connection at once, which would lose the close code. Shutdown goes
// through closeWith and a proper Close instead.
type conn struct {
	srv  *Server
	ws   *websocket.Conn
	id   string
	uid  int64
	ctrl chan []byte

	cancel    context.CancelFunc
	closeOnce sync.Once
	code      websocket.StatusCode
	reason    string
	running   bool // guarded by mu
	mu        sync.Mutex
}

// flushWait bounds how long closing waits for the writer to flush (a network timeout, so
// real time, not the injectable clock).
const flushWait = 5 * time.Second

// outStream hands the writer the scrollback and the live output once the lab is running.
type outStream struct {
	scrollback []byte
	ch         <-chan []byte
}

// closeWith ends the connection with code and reason (first caller wins).
func (c *conn) closeWith(code websocket.StatusCode, reason string) {
	c.closeOnce.Do(func() {
		c.code, c.reason = code, reason
		c.cancel()
	})
}

func (c *conn) replaced() { c.closeWith(websocket.StatusNormalClosure, "replaced") }

func (c *conn) setRunning(v bool) {
	c.mu.Lock()
	c.running = v
	c.mu.Unlock()
}

func (c *conn) isRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// sendCtrl queues a control frame for the writer; it gives up when the connection ends.
func (c *conn) sendCtrl(ctx context.Context, v any) {
	select {
	case c.ctrl <- mustJSON(v):
	case <-ctx.Done():
	}
}

func (c *conn) run(parent context.Context, first orch.SessionInfo) {
	s := c.srv
	ctx, cancel := context.WithCancel(context.WithoutCancel(parent))
	c.cancel = cancel
	s.takeOver(c)

	outReady := make(chan outStream, 1)
	writerDone, readerDone := make(chan struct{}), make(chan struct{})
	go func() { defer close(writerDone); c.writeLoop(ctx, outReady) }()
	go func() { defer close(readerDone); c.readLoop(ctx) }()

	gen := -1
	var cancelSub func()
	defer func() {
		c.closeWith(websocket.StatusNormalClosure, "")
		if cancelSub != nil {
			cancelSub() // unblocks the terminal if the writer had fallen behind
		}
		if gen >= 0 {
			s.o.Sessions.ClientDetached(c.id, gen)
		}
		s.release(c)
		// The writer flushes the last control frames (state: ended) before the close frame.
		// A client that stopped reading can hold the writer in a write forever; after
		// flushWait the close goes ahead, and closing the connection releases the writer.
		select {
		case <-writerDone:
		case <-time.After(flushWait):
		}
		_ = c.ws.Close(c.code, c.reason)
		<-writerDone
		<-readerDone
	}()

	ended, final, err := s.o.Sessions.Ended(c.id)
	if err != nil {
		c.closeWith(websocket.StatusNormalClosure, "ended")
		return
	}
	endedNow := func() {
		in := final()
		c.sendCtrl(ctx, stateFrame{Type: "state", State: "ended", Reason: in.EndReason})
		s.forget(c.id)
		c.closeWith(websocket.StatusNormalClosure, "ended")
	}

	// Wait for a slot and for the container, reporting the queue position as it changes.
	info, lastPos := first, -1
	for info.State != orch.StateRunning {
		switch info.State {
		case orch.StateQueued:
			if info.QueuePosition != lastPos {
				lastPos = info.QueuePosition
				c.sendCtrl(ctx, queuedFrame{Type: "queued", Position: lastPos})
			}
		case orch.StateCreating:
		default:
			endedNow()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ended:
			endedNow()
			return
		case <-s.o.Clock.After(s.o.QueuePoll):
		}
		if info, err = s.o.Sessions.Get(c.id); err != nil {
			endedNow()
			return
		}
	}

	if gen, err = s.o.Sessions.ClientAttached(c.id); err != nil {
		gen = -1
		endedNow()
		return
	}
	out, err := s.o.Sessions.Output(c.id)
	if err != nil {
		endedNow()
		return
	}
	scrollback, ch, cs := out.Subscribe(s.o.OutDepth)
	cancelSub = cs
	c.setRunning(true)
	c.sendCtrl(ctx, stateFrame{Type: "state", State: "running"})
	outReady <- outStream{scrollback: scrollback, ch: ch}
	c.sendTTL(ctx)

	ttl := s.o.Clock.NewTimer(s.o.TTLEvery)
	defer ttl.Stop()
	watch := s.o.Clock.NewTimer(time.Second)
	defer watch.Stop()
	var fullSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ended:
			endedNow()
			return
		case <-ttl.C():
			c.sendTTL(ctx)
			ttl.Reset(s.o.TTLEvery)
		case <-watch.C():
			// Slow consumer: output backed up for SlowConsumer. The lab stays alive for the
			// reconnect grace; the terminal resumes as soon as the subscription is cancelled.
			now := s.o.Clock.Now()
			if len(ch) < cap(ch) {
				fullSince = time.Time{}
			} else if fullSince.IsZero() {
				fullSince = now
			} else if now.Sub(fullSince) >= s.o.SlowConsumer {
				s.o.Log.Warn("ws: slow consumer; closing", "session_id", c.id)
				c.closeWith(websocket.StatusPolicyViolation, "slow_consumer")
				return
			}
			watch.Reset(time.Second)
		}
	}
}

func (c *conn) sendTTL(ctx context.Context) {
	in, err := c.srv.o.Sessions.Get(c.id)
	if err != nil {
		return
	}
	now := c.srv.o.Clock.Now()
	secs := func(t time.Time) int {
		if t.IsZero() {
			return 0
		}
		return max(int(t.Sub(now).Seconds()), 0)
	}
	c.sendCtrl(ctx, ttlFrame{Type: "ttl", IdleRemainingS: secs(in.IdleDeadline),
		HardRemainingS: secs(in.HardDeadline), ExtendAvailable: !in.Extended})
}

// readLoop handles everything the browser sends.
func (c *conn) readLoop(ctx context.Context) {
	s := c.srv
	for {
		typ, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.closeWith(websocket.StatusNormalClosure, "") // the client went away
			return
		}
		switch typ {
		case websocket.MessageBinary:
			c.input(ctx, data)
		case websocket.MessageText:
			var f clientFrame
			if json.Unmarshal(data, &f) != nil {
				s.unknownFrames.Add(1)
				continue
			}
			switch f.Type {
			case "resize":
				if c.isRunning() && f.Cols > 0 && f.Rows > 0 && f.Cols <= 1000 && f.Rows <= 1000 {
					rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					if err := s.o.Sessions.Resize(rctx, c.id, f.Cols, f.Rows); err != nil {
						s.o.Log.Debug("ws: resize", "session_id", c.id, "err", err)
					}
					cancel()
				}
			case "extend":
				_, err := s.o.Sessions.Extend(c.id)
				c.sendCtrl(ctx, extendFrame{Type: "extend", OK: err == nil})
			case "ping":
				// Keeps the socket open; deliberately not activity (spec).
			default:
				s.unknownFrames.Add(1)
			}
		}
	}
}

// input rate-limits keystrokes, captures command lines and writes them to the terminal.
func (c *conn) input(ctx context.Context, data []byte) {
	s := c.srv
	if !c.isRunning() {
		return // keystrokes while queued go nowhere
	}
	st := s.termState(c.id)
	st.mu.Lock()
	now := s.o.Clock.Now()
	allowed, warn := st.in.admit(now, len(data))
	data = data[:allowed]
	lines := st.split.Feed(data)
	firstSeq := st.seq + 1
	st.seq += len(lines)
	st.mu.Unlock()

	if warn {
		c.sendCtrl(ctx, warnFrame{Type: "warn", Message: "input rate limit: keystrokes dropped"})
	}
	if len(data) > 0 {
		if err := s.o.Sessions.WriteInput(c.id, data); err != nil {
			return
		}
	}
	if len(lines) == 0 {
		return
	}
	s.o.Sessions.AddCommands(c.id, len(lines))
	if s.o.Recorder == nil {
		return
	}
	info, _ := s.o.Sessions.Get(c.id)
	for i, l := range lines {
		d := map[string]any{"seq": firstSeq + i, "line": l.Text}
		if l.Truncated {
			d["truncated"] = true
		}
		s.o.Recorder.Record(store.Event{TS: now, Type: "command_entered", UserID: c.uid,
			SessionID: c.id, ChallengeSlug: info.ChallengeSlug, Data: d})
	}
}

// writeLoop is the connection's only writer.
func (c *conn) writeLoop(ctx context.Context, outReady <-chan outStream) {
	s := c.srv
	lim := newOutputLimiter(s.o.OutputRate, s.o.OutputBurst, s.o.Clock)
	var out <-chan []byte
	writeBinary := func(p []byte) bool {
		for len(p) > 0 {
			n := min(len(p), s.o.OutputBurst)
			if lim.wait(ctx, n) != nil {
				return false
			}
			if c.ws.Write(context.Background(), websocket.MessageBinary, p[:n]) != nil {
				c.closeWith(websocket.StatusNormalClosure, "")
				return false
			}
			p = p[n:]
		}
		return true
	}
	for {
		// Control frames first, so ended/warn/ttl are not stuck behind a burst of output.
		select {
		case f := <-c.ctrl:
			if c.ws.Write(context.Background(), websocket.MessageText, f) != nil {
				c.closeWith(websocket.StatusNormalClosure, "")
				return
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			// Deliver the last control frames (e.g. state: ended) before the close frame.
			for {
				select {
				case f := <-c.ctrl:
					_ = c.ws.Write(context.Background(), websocket.MessageText, f)
				default:
					return
				}
			}
		case f := <-c.ctrl:
			if c.ws.Write(context.Background(), websocket.MessageText, f) != nil {
				c.closeWith(websocket.StatusNormalClosure, "")
				return
			}
		case os := <-outReady:
			out = os.ch
			if !writeBinary(os.scrollback) {
				return
			}
		case p := <-out:
			if !writeBinary(p) {
				return
			}
		}
	}
}
