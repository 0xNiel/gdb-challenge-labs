package clock

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func TestFake_TimerFiresOnAdvance(t *testing.T) {
	t.Parallel()
	c := NewFake(t0)
	tm := c.NewTimer(10 * time.Second)
	c.Advance(9 * time.Second)
	select {
	case <-tm.C():
		t.Fatal("fired early")
	default:
	}
	c.Advance(time.Second)
	select {
	case got := <-tm.C():
		if !got.Equal(t0.Add(10 * time.Second)) {
			t.Fatalf("fired at %v", got)
		}
	default:
		t.Fatal("did not fire at its deadline")
	}
	if c.Pending() != 0 {
		t.Fatalf("pending %d after firing", c.Pending())
	}
}

func TestFake_AfterFuncStopReset(t *testing.T) {
	t.Parallel()
	c := NewFake(t0)
	var order []string
	a := c.AfterFunc(5*time.Second, func() { order = append(order, "a") })
	c.AfterFunc(3*time.Second, func() { order = append(order, "b") })
	stopped := c.AfterFunc(1*time.Second, func() { order = append(order, "stopped") })
	if !stopped.Stop() {
		t.Fatal("Stop on a pending timer returned false")
	}
	a.Reset(7 * time.Second) // now at t0+7s
	c.Advance(6 * time.Second)
	if len(order) != 1 || order[0] != "b" {
		t.Fatalf("after 6s: %v", order)
	}
	c.Advance(time.Second)
	if len(order) != 2 || order[1] != "a" {
		t.Fatalf("after 7s: %v", order)
	}
	if !c.Now().Equal(t0.Add(7 * time.Second)) {
		t.Fatalf("now %v", c.Now())
	}
}

func TestFake_CallbackMayArmTimers(t *testing.T) {
	t.Parallel()
	c := NewFake(t0)
	fired := 0
	var rearm func()
	rearm = func() {
		fired++
		if fired < 3 {
			c.AfterFunc(time.Second, rearm)
		}
	}
	c.AfterFunc(time.Second, rearm)
	c.Advance(10 * time.Second)
	if fired != 3 {
		t.Fatalf("fired %d times, want 3", fired)
	}
}
