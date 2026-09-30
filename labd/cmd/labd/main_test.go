package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestServeHTTP_ShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { done <- serveHTTP(ctx, "test", ln, h, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveHTTP returned %v", err)
		}
	case <-time.After(shutdownDrain + time.Second):
		t.Fatal("serveHTTP did not return after cancel")
	}
}

// A WebSocket is a hijacked connection, which http.Server.Shutdown neither closes nor waits
// for. serveAll must still return promptly, after closeWS has closed the socket with a close
// frame (the client sees 1000, not a dropped connection).
func TestServeAll_ClosesWebSocketsOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var open []*websocket.Conn
	var handlers sync.WaitGroup
	ws := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlers.Add(1)
		defer handlers.Done()
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		open = append(open, c)
		mu.Unlock()
		for {
			if _, _, err := c.Read(context.Background()); err != nil {
				return
			}
		}
	})
	closeWS := func(ctx context.Context) error {
		mu.Lock()
		for _, c := range open {
			_ = c.Close(websocket.StatusNormalClosure, "")
		}
		mu.Unlock()
		handlers.Wait()
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- serveAll(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), closeWS, listener{"ws", ln, ws})
	}()

	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	client, _, err := websocket.Dial(dctx, "ws://"+ln.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	readErr := make(chan error, 1)
	go func() {
		_, _, err := client.Read(context.Background())
		readErr <- err
	}()

	t0 := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serveAll returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("serveAll did not return within 3 s of cancel with a WebSocket open")
	}
	t.Logf("serveAll returned %v after cancel", time.Since(t0))
	select {
	case err := <-readErr:
		var ce websocket.CloseError
		if !errors.As(err, &ce) || ce.Code != websocket.StatusNormalClosure {
			t.Fatalf("client read ended with %v, want a 1000 close frame", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("client never saw the socket close")
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	if err := run([]string{"-config", "../../labd.example.yaml", "frobnicate"}, nil); err == nil {
		t.Fatal("unknown command accepted")
	}
}

func TestRun_ServeRefusesWithoutSecret(t *testing.T) {
	t.Setenv("LABD_INTERNAL_SECRET", "")
	err := run([]string{"-config", "../../labd.example.yaml", "serve"}, nil)
	if err == nil || !strings.Contains(err.Error(), "LABD_INTERNAL_SECRET") {
		t.Fatalf("err %v, want a refusal naming LABD_INTERNAL_SECRET", err)
	}
}
