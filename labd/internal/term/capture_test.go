package term

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/store"
)

func TestSplitter(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", 5*1024)
	for name, c := range map[string]struct {
		in   []string // fed in separate chunks
		want []Line
	}{
		"cr": {[]string{"break main\r"}, []Line{{Text: "break main"}}},
		// The plan's example "ne\x7fxt\n" contradicts its own rule: backspace after "ne" leaves
		// "n", so it yields "nxt" (checked below). README.md records the correction.
		"backspace":         {[]string{"nexx\x7ft\n"}, []Line{{Text: "next"}}},
		"plan example":      {[]string{"ne\x7fxt\n"}, []Line{{Text: "nxt"}}},
		"ctrl-h":            {[]string{"nexx\x08t\n"}, []Line{{Text: "next"}}},
		"arrow then text":   {[]string{"\x1b[Anext\n"}, []Line{{Text: "next"}}},
		"ss3 arrow":         {[]string{"\x1bOAstep\r"}, []Line{{Text: "step"}}},
		"osc":               {[]string{"\x1b]0;title\x07bt\r", "\x1b]2;x\x1b\\up\r"}, []Line{{Text: "bt"}, {Text: "up"}}},
		"ctrl-c":            {[]string{"\x03"}, []Line{{Text: "^C"}}},
		"ctrl-c drops line": {[]string{"pri\x03print x\r"}, []Line{{Text: "^C"}, {Text: "print x"}}},
		"empty lines":       {[]string{"\r\r", "\n", "   \r"}, nil},
		"crlf":              {[]string{"run\r\n"}, []Line{{Text: "run"}}},
		"split sequence":    {[]string{"li", "\x1b[", "D", "st\r"}, []Line{{Text: "list"}}},
		"ctrl-u":            {[]string{"garbage\x15info locals\r"}, []Line{{Text: "info locals"}}},
		"tab dropped":       {[]string{"brea\tk main\r"}, []Line{{Text: "break main"}}},
		"utf8 backspace":    {[]string{"print \"é\x7f\"\r"}, []Line{{Text: "print \"\""}}},
		"long line":         {[]string{long}, []Line{{Text: long[:MaxLine+1], Truncated: true}}},
	} {
		var s Splitter
		var got []Line
		for _, chunk := range c.in {
			got = append(got, s.Feed([]byte(chunk))...)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %d lines %+v, want %+v", name, len(got), got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: line %d = %+v, want %+v", name, i, got[i], c.want[i])
			}
		}
	}
}

func TestRecorder_BatchesAndFlushes(t *testing.T) {
	t.Parallel()
	st := store.NewMemory()
	clk := clock.NewFake(t0)
	r := NewRecorder(st, clk, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for i := 1; i <= 3; i++ {
		r.Record(store.Event{Type: "command_entered", SessionID: "s", Data: map[string]any{"seq": i}})
	}
	time.Sleep(20 * time.Millisecond)
	if n := len(st.Events()); n != 0 {
		t.Fatalf("%d events written before the flush interval", n)
	}
	clk.Advance(time.Second)
	waitUntil(t, func() bool { return len(st.Events()) == 3 })
	for i := range 150 {
		r.Record(store.Event{Type: "command_entered", SessionID: "s", Data: map[string]any{"seq": 4 + i}})
	}
	waitUntil(t, func() bool { return len(st.Events()) >= 103 }) // a full batch without a tick
	r.Close()
	evs := st.Events()
	if len(evs) != 153 {
		t.Fatalf("%d events after Close, want 153", len(evs))
	}
	for i, e := range evs {
		if e.Data["seq"] != i+1 {
			t.Fatalf("event %d has seq %v", i, e.Data["seq"])
		}
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met within 3 s")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRecorder_RecordAfterCloseIsSafe(t *testing.T) {
	t.Parallel()
	r := NewRecorder(store.NewMemory(), clock.NewFake(t0), slog.New(slog.NewTextHandler(io.Discard, nil)))
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				r.Record(store.Event{Type: "command_entered"})
			}
		}
	}()
	time.Sleep(10 * time.Millisecond)
	r.Close()
	if r.Record(store.Event{}) {
		t.Fatal("Record after Close reported success")
	}
	close(stop)
}
