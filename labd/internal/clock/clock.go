// Package clock abstracts time so session timers (idle, hard TTL, queue abandonment) can be
// tested without waiting. Production code uses Real; tests use Fake and call Advance.
package clock

import (
	"sort"
	"sync"
	"time"
)

// Clock is the time source labd depends on.
type Clock interface {
	Now() time.Time
	// NewTimer fires once on its channel after d.
	NewTimer(d time.Duration) Timer
	// After is NewTimer(d).C().
	After(d time.Duration) <-chan time.Time
	// AfterFunc runs f in its own goroutine after d.
	AfterFunc(d time.Duration, f func()) Timer
}

// Timer is the subset of *time.Timer labd uses. C is nil for AfterFunc timers.
type Timer interface {
	C() <-chan time.Time
	// Stop prevents the timer from firing; it reports whether it was still pending.
	Stop() bool
	// Reset re-arms the timer to fire after d; it reports whether it was still pending.
	Reset(d time.Duration) bool
}

// Real is the wall clock.
type Real struct{}

func (Real) Now() time.Time                         { return time.Now() }
func (Real) NewTimer(d time.Duration) Timer         { return realTimer{time.NewTimer(d)} }
func (Real) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (Real) AfterFunc(d time.Duration, f func()) Timer {
	return realTimer{time.AfterFunc(d, f)}
}

type realTimer struct{ t *time.Timer }

func (r realTimer) C() <-chan time.Time        { return r.t.C }
func (r realTimer) Stop() bool                 { return r.t.Stop() }
func (r realTimer) Reset(d time.Duration) bool { return r.t.Reset(d) }

// Fake is a manually advanced clock. Timers fire during Advance, in deadline order.
// AfterFunc callbacks run synchronously inside Advance, so a test sees their effects as soon
// as Advance returns.
type Fake struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

// NewFake returns a Fake clock set to start.
func NewFake(start time.Time) *Fake { return &Fake{now: start} }

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *Fake) NewTimer(d time.Duration) Timer {
	t := &fakeTimer{clk: f, ch: make(chan time.Time, 1)}
	t.Reset(d)
	return t
}

func (f *Fake) After(d time.Duration) <-chan time.Time { return f.NewTimer(d).C() }

func (f *Fake) AfterFunc(d time.Duration, fn func()) Timer {
	t := &fakeTimer{clk: f, fn: fn}
	t.Reset(d)
	return t
}

// Advance moves the clock forward by d, firing every timer whose deadline is reached.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	target := f.now.Add(d)
	f.mu.Unlock()
	for {
		f.mu.Lock()
		sort.SliceStable(f.timers, func(i, j int) bool { return f.timers[i].when.Before(f.timers[j].when) })
		if len(f.timers) == 0 || f.timers[0].when.After(target) {
			f.now = target
			f.mu.Unlock()
			return
		}
		t := f.timers[0]
		f.timers = f.timers[1:]
		f.now = t.when
		now := f.now
		f.mu.Unlock()
		if t.fn != nil {
			t.fn()
		} else {
			select {
			case t.ch <- now:
			default:
			}
		}
	}
}

// Pending is the number of armed timers (for tests that check timers were stopped).
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.timers)
}

type fakeTimer struct {
	clk  *Fake
	ch   chan time.Time
	fn   func()
	when time.Time
}

func (t *fakeTimer) C() <-chan time.Time { return t.ch }

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	return t.removeLocked()
}

func (t *fakeTimer) Reset(d time.Duration) bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	was := t.removeLocked()
	t.when = t.clk.now.Add(d)
	t.clk.timers = append(t.clk.timers, t)
	return was
}

func (t *fakeTimer) removeLocked() bool {
	for i, x := range t.clk.timers {
		if x == t {
			t.clk.timers = append(t.clk.timers[:i], t.clk.timers[i+1:]...)
			return true
		}
	}
	return false
}
