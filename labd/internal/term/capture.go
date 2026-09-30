package term

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gdblabs/labd/internal/clock"
	"gdblabs/labd/internal/store"
)

// MaxLine is the longest line the splitter buffers; a longer one is emitted truncated.
const MaxLine = 4096

// Line is one captured command line.
type Line struct {
	Text      string
	Truncated bool
}

// Splitter turns the raw keystrokes of a terminal into command lines (spec "Command
// capture"): ANSI CSI, OSC and SS3 sequences and lone control bytes are dropped, backspace
// deletes the previous rune, Ctrl-U clears the line, CR or LF ends it, Ctrl-C is recorded as
// "^C". It captures what was typed, not what gdb made of it (history recall is invisible).
type Splitter struct {
	buf   []byte
	state int
}

const (
	stNormal = iota
	stEsc    // after ESC
	stCSI    // ESC [ ... final byte 0x40-0x7e
	stOSC    // ESC ] ... BEL or ESC \
	stOSCEsc // ESC inside OSC
	stSS3    // ESC O x
)

// Feed consumes bytes and returns the lines they completed.
func (s *Splitter) Feed(p []byte) []Line {
	var out []Line
	emit := func() {
		if t := strings.TrimSpace(string(s.buf)); t != "" {
			out = append(out, Line{Text: t})
		}
		s.buf = s.buf[:0]
	}
	for _, b := range p {
		switch s.state {
		case stEsc:
			switch b {
			case '[':
				s.state = stCSI
			case ']':
				s.state = stOSC
			case 'O':
				s.state = stSS3
			default:
				s.state = stNormal // two-byte escape: drop both
			}
			continue
		case stCSI:
			if b >= 0x40 && b <= 0x7e {
				s.state = stNormal
			}
			continue
		case stOSC:
			switch b {
			case 0x07:
				s.state = stNormal
			case 0x1b:
				s.state = stOSCEsc
			}
			continue
		case stOSCEsc:
			s.state = stOSC
			if b == '\\' {
				s.state = stNormal
			}
			continue
		case stSS3:
			s.state = stNormal
			continue
		}
		switch {
		case b == 0x1b:
			s.state = stEsc
		case b == '\r' || b == '\n':
			emit()
		case b == 0x03:
			s.buf = s.buf[:0]
			out = append(out, Line{Text: "^C"})
		case b == 0x15: // Ctrl-U: readline kills the line
			s.buf = s.buf[:0]
		case b == 0x7f || b == 0x08:
			if len(s.buf) > 0 {
				_, n := utf8.DecodeLastRune(s.buf)
				s.buf = s.buf[:len(s.buf)-n]
			}
		case b < 0x20:
			// other control bytes (tab completion, Ctrl-D, ...) are not part of the line
		default:
			s.buf = append(s.buf, b)
			if len(s.buf) > MaxLine {
				t := strings.TrimSpace(string(s.buf))
				s.buf = s.buf[:0]
				out = append(out, Line{Text: t, Truncated: true})
			}
		}
	}
	return out
}

// EventSink is where captured commands go (the store's InsertEvents).
type EventSink interface {
	InsertEvents(ctx context.Context, evs ...store.Event) error
}

// Recorder batches command_entered events and writes them every second or every 100 events.
type Recorder struct {
	sink EventSink
	clk  clock.Clock
	log  *slog.Logger
	in   chan store.Event
	done chan struct{}

	mu     sync.RWMutex // Record holds it shared; Close exclusively, so no send hits a closed channel
	closed bool
}

const (
	recordBatch    = 100
	recordInterval = time.Second
	recordQueue    = 4096 // events waiting to be written; beyond it new ones are dropped
)

// NewRecorder starts the writer goroutine; Close flushes and stops it.
func NewRecorder(sink EventSink, clk clock.Clock, log *slog.Logger) *Recorder {
	r := &Recorder{sink: sink, clk: clk, log: log, in: make(chan store.Event, recordQueue), done: make(chan struct{})}
	go r.run()
	return r
}

// Record queues one event without blocking. It reports false if the queue was full or the
// recorder is closed (a connection can outlive labd's shutdown by a moment).
func (r *Recorder) Record(ev store.Event) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return false
	}
	select {
	case r.in <- ev:
		return true
	default:
		r.log.Warn("command capture queue full; event dropped", "session_id", ev.SessionID)
		return false
	}
}

func (r *Recorder) run() {
	defer close(r.done)
	batch := make([]store.Event, 0, recordBatch)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := r.sink.InsertEvents(ctx, batch...); err != nil {
			r.log.Error("write command_entered events", "n", len(batch), "err", err)
		}
		cancel()
		batch = batch[:0]
	}
	tick := r.clk.NewTimer(recordInterval)
	for {
		select {
		case ev, ok := <-r.in:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ev)
			if len(batch) >= recordBatch {
				flush()
			}
		case <-tick.C():
			flush()
			tick.Reset(recordInterval)
		}
	}
}

// Close writes what is queued and stops the recorder.
func (r *Recorder) Close() {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.in)
	}
	r.mu.Unlock()
	<-r.done
}
