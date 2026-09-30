// Package client is a scripted terminal client for labd's WebSocket gateway: tests, the P1
// replay command and (Phase 4) the load driver use it. It speaks the protocol in
// labd/internal/term/README.md.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Client is one WebSocket session.
type Client struct {
	ws *websocket.Conn

	mu       sync.Mutex
	out      bytes.Buffer // output not yet consumed by Expect
	notify   chan struct{}
	ctrl     []map[string]any
	closeErr error

	sentAt  time.Time // last Send; zero once its echo arrived
	lat     []time.Duration
	bytesIn int64 // terminal bytes received
	bytesOt int64 // terminal bytes sent
}

// Options for Dial.
type Options struct {
	Origin string // sent as the Origin header when set
}

// Dial opens the socket: url is ws(s)://host/ws/term/<session_id>, token the ws_token.
func Dial(ctx context.Context, url, token string, o Options) (*Client, error) {
	hdr := http.Header{}
	if o.Origin != "" {
		hdr.Set("Origin", o.Origin)
	}
	ws, resp, err := websocket.Dial(ctx, url+"?t="+token, &websocket.DialOptions{HTTPHeader: hdr})
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial: HTTP %d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("dial: %w", err)
	}
	ws.SetReadLimit(4 << 20)
	c := &Client{ws: ws, notify: make(chan struct{}, 1)}
	go c.read()
	return c, nil
}

func (c *Client) read() {
	for {
		typ, data, err := c.ws.Read(context.Background())
		c.mu.Lock()
		if err != nil {
			c.closeErr = err
			c.mu.Unlock()
			c.wake()
			return
		}
		if typ == websocket.MessageBinary {
			if !c.sentAt.IsZero() {
				c.lat = append(c.lat, time.Since(c.sentAt))
				c.sentAt = time.Time{}
			}
			c.bytesIn += int64(len(data))
			c.out.Write(data)
		} else {
			var m map[string]any
			if json.Unmarshal(data, &m) == nil {
				c.ctrl = append(c.ctrl, m)
			}
		}
		c.mu.Unlock()
		c.wake()
	}
}

func (c *Client) wake() {
	select {
	case c.notify <- struct{}{}:
	default:
	}
}

// Send types line followed by a newline. The time to the first output byte after it is
// recorded as that command's echo latency.
func (c *Client) Send(line string) error { return c.SendRaw([]byte(line + "\n")) }

// SendRaw sends keystrokes as they are.
func (c *Client) SendRaw(p []byte) error {
	c.mu.Lock()
	c.sentAt = time.Now()
	c.bytesOt += int64(len(p))
	c.mu.Unlock()
	return c.ws.Write(context.Background(), websocket.MessageBinary, p)
}

// ErrClosed is returned by Expect and WaitControl once the socket has closed.
var ErrClosed = errors.New("connection closed")

// Expect waits until the unconsumed output matches re, consumes it up to the end of the
// match and returns everything consumed.
func (c *Client) Expect(re *regexp.Regexp, timeout time.Duration) (string, error) {
	deadline := time.After(timeout)
	for {
		c.mu.Lock()
		if loc := re.FindIndex(c.out.Bytes()); loc != nil {
			got := string(c.out.Next(loc[1]))
			c.mu.Unlock()
			return got, nil
		}
		closed := c.closeErr
		c.mu.Unlock()
		if closed != nil {
			return "", fmt.Errorf("%w: %v", ErrClosed, closed)
		}
		select {
		case <-c.notify:
		case <-deadline:
			c.mu.Lock()
			tail := c.out.String()
			c.mu.Unlock()
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			return "", fmt.Errorf("no match for %q within %s; output ends with %q", re, timeout, tail)
		}
	}
}

// WaitControl waits for the next control frame of type typ that arrived after the last one
// consumed, and consumes it.
func (c *Client) WaitControl(typ string, timeout time.Duration) (map[string]any, error) {
	deadline := time.After(timeout)
	for {
		c.mu.Lock()
		for i, m := range c.ctrl {
			if m["type"] == typ {
				c.ctrl = append(c.ctrl[:i:i], c.ctrl[i+1:]...)
				c.mu.Unlock()
				return m, nil
			}
		}
		closed := c.closeErr
		c.mu.Unlock()
		if closed != nil {
			return nil, fmt.Errorf("%w: %v", ErrClosed, closed)
		}
		select {
		case <-c.notify:
		case <-deadline:
			return nil, fmt.Errorf("no %q frame within %s", typ, timeout)
		}
	}
}

func (c *Client) sendCtrl(v any) error {
	b, _ := json.Marshal(v)
	return c.ws.Write(context.Background(), websocket.MessageText, b)
}

// Resize sends a resize frame.
func (c *Client) Resize(cols, rows int) error {
	return c.sendCtrl(map[string]any{"type": "resize", "cols": cols, "rows": rows})
}

// Ping sends a ping frame (keeps the socket open; not activity).
func (c *Client) Ping() error { return c.sendCtrl(map[string]string{"type": "ping"}) }

// Extend asks for the one extension and reports whether it was granted.
func (c *Client) Extend(timeout time.Duration) (bool, error) {
	if err := c.sendCtrl(map[string]string{"type": "extend"}); err != nil {
		return false, err
	}
	m, err := c.WaitControl("extend", timeout)
	if err != nil {
		return false, err
	}
	ok, _ := m["ok"].(bool)
	return ok, nil
}

// Latencies returns the recorded echo latencies, one per Send whose echo arrived.
func (c *Client) Latencies() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.lat...)
}

// Bytes returns terminal bytes received and sent.
func (c *Client) Bytes() (in, out int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytesIn, c.bytesOt
}

// CloseStatus returns the close code once the socket has closed (-1 before, or if the
// connection ended without a close frame).
func (c *Client) CloseStatus() websocket.StatusCode {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closeErr == nil {
		return -1
	}
	return websocket.CloseStatus(c.closeErr)
}

// Close closes the socket normally.
func (c *Client) Close() error { return c.ws.Close(websocket.StatusNormalClosure, "") }
