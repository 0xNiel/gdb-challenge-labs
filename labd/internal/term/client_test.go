package term

import (
	"context"
	"regexp"
	"testing"
	"time"

	"gdblabs/labd/internal/term/client"
)

// TestClient_BreakMain is task 3.7: the scripted client against the fake server.
func TestClient_BreakMain(t *testing.T) {
	t.Parallel()
	h := newHarness(t, 1, false, nil)
	id := h.start(t, 1)
	c, err := client.Dial(context.Background(), h.url(id), h.tokens.Mint(id, 1, h.clk.Now()), client.Options{Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if st, err := c.WaitControl("state", 5*time.Second); err != nil || st["state"] != "running" {
		t.Fatalf("state %v, %v", st, err)
	}
	pty := h.rt.get(id)
	if err := c.Send("break main"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Expect(regexp.MustCompile(`break main\n`), 5*time.Second); err != nil {
		t.Fatal(err)
	}
	pty.emit([]byte("Breakpoint 1 at 0x401342: file perf.c, line 107.\n(gdb) "))
	got, err := c.Expect(regexp.MustCompile(`Breakpoint 1`), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got == "" {
		t.Fatal("empty match")
	}
	if lat := c.Latencies(); len(lat) != 1 || lat[0] <= 0 {
		t.Fatalf("latencies %v, want one positive", lat)
	}
	if ok, err := c.Extend(5 * time.Second); err != nil || !ok {
		t.Fatalf("extend %v %v", ok, err)
	}
	if err := c.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	in, out := c.Bytes()
	if in == 0 || out != int64(len("break main\n")) {
		t.Fatalf("bytes in %d out %d", in, out)
	}
	if _, err := c.Expect(regexp.MustCompile(`never`), 50*time.Millisecond); err == nil {
		t.Fatal("Expect matched text that never came")
	}
}
