package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeHTTP_ShutsDownOnCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	go func() { done <- serveHTTP(ctx, ln, h, slog.New(slog.NewTextHandler(io.Discard, nil))) }()

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
