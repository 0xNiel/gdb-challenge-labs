package term

import (
	"context"
	"time"

	"golang.org/x/time/rate"

	"gdblabs/labd/internal/clock"
)

// Spec "Terminal gateway → Rate limits" (S13).
const (
	InputRate   = 2048   // bytes/s sustained, per session
	InputBurst  = 16384  // bytes
	OutputRate  = 262144 // bytes/s, per connection
	OutputBurst = 32768  // bytes; also the largest binary frame the gateway sends
	warnEvery   = time.Second
)

// inputLimiter drops keystrokes beyond the token bucket and says when to warn.
type inputLimiter struct {
	lim      *rate.Limiter
	lastWarn time.Time
}

func newInputLimiter(r, burst int) *inputLimiter {
	return &inputLimiter{lim: rate.NewLimiter(rate.Limit(r), burst)}
}

// admit returns how many of n bytes may pass now, and whether to send a warn frame (at most
// one per second while bytes are being dropped).
func (l *inputLimiter) admit(now time.Time, n int) (allowed int, warn bool) {
	allowed = min(n, int(l.lim.TokensAt(now)))
	if allowed > 0 {
		l.lim.AllowN(now, allowed)
	}
	if allowed < n && now.Sub(l.lastWarn) >= warnEvery {
		l.lastWarn = now
		warn = true
	}
	return allowed, warn
}

// outputLimiter paces output with the injectable clock (so tests control time).
type outputLimiter struct {
	lim *rate.Limiter
	clk clock.Clock
}

func newOutputLimiter(r, burst int, clk clock.Clock) *outputLimiter {
	return &outputLimiter{lim: rate.NewLimiter(rate.Limit(r), burst), clk: clk}
}

// wait blocks until n bytes (n <= burst) may be sent.
func (l *outputLimiter) wait(ctx context.Context, n int) error {
	now := l.clk.Now()
	d := l.lim.ReserveN(now, n).DelayFrom(now)
	if d <= 0 {
		return nil
	}
	select {
	case <-l.clk.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
