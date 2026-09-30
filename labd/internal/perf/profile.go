package perf

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// Script is session.gdb split at its "# profile: <name>" tags, in file order.
type Script struct {
	Order    []string
	Sections map[string][]string
}

// ParseScript reads session.gdb: one gdb command per line, "#" comments, blank lines
// skipped, and "# profile: X" starting section X.
func ParseScript(r io.Reader) (Script, error) {
	s := Script{Sections: map[string][]string{}}
	cur := ""
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if name, ok := strings.CutPrefix(l, "# profile:"); ok {
			cur = strings.TrimSpace(name)
			if _, dup := s.Sections[cur]; dup {
				return Script{}, fmt.Errorf("section %q appears twice", cur)
			}
			s.Order = append(s.Order, cur)
			s.Sections[cur] = nil
			continue
		}
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if cur == "" {
			return Script{}, fmt.Errorf("command %q before the first \"# profile:\" tag", l)
		}
		s.Sections[cur] = append(s.Sections[cur], l)
	}
	return s, sc.Err()
}

// LoadScript parses the file at path.
func LoadScript(path string) (Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return Script{}, err
	}
	defer f.Close()
	s, err := ParseScript(f)
	if err != nil {
		return Script{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Profile is one kind of simulated learner (spec "Load driver").
type Profile struct {
	Name     string
	Setup    []string // sent once after gdb starts
	Loop     []string // then cycled until the hold ends
	PerMin   float64  // commands per minute, averaged over wall time
	IdleFrac float64  // share of wall time with no commands at all
	Abuse    bool     // runs the abuse actions instead of Loop, then idles
}

// Profile names.
const (
	Reader  = "reader"
	Stepper = "stepper"
	Abuser  = "abuser"
)

// preamble keeps an interactive gdb from ever waiting on a question: a second `run`, a
// long backtrace or `delete` would otherwise block on "(y or n)" or "--Type <RET>".
var preamble = []string{"set confirm off", "set pagination off"}

// Profiles builds the spec's three profiles from session.gdb.
//   - reader: 2 commands/min from the reader section (list, info locals, bt ...), 30 % idle.
//     The setup section runs first, so the program sits at the breakpoint in main.
//   - stepper: 20 commands/min cycling every section but reader: next/step/print loops, the
//     thread section and one watch. This is the P1 workload of Phase 3.
//   - abuser: the four abuse actions (vuser.go), then idle.
func Profiles(s Script) (map[string]Profile, error) {
	for _, need := range []string{"setup", "reader", "stepper", "watch"} {
		if len(s.Sections[need]) == 0 {
			return nil, fmt.Errorf("session.gdb has no %q section", need)
		}
	}
	var step []string
	for _, name := range s.Order {
		if name != Reader {
			step = append(step, s.Sections[name]...)
		}
	}
	with := func(cmds ...[]string) []string {
		out := append([]string(nil), preamble...)
		for _, c := range cmds {
			out = append(out, c...)
		}
		return out
	}
	return map[string]Profile{
		Reader:  {Name: Reader, Setup: with(s.Sections["setup"]), Loop: s.Sections[Reader], PerMin: 2, IdleFrac: 0.3},
		Stepper: {Name: Stepper, Setup: with(), Loop: step, PerMin: 20},
		Abuser:  {Name: Abuser, Setup: with(), Abuse: true},
	}, nil
}

// idlePeriod is the length of one active-then-idle cycle for profiles with IdleFrac.
const idlePeriod = 5 * time.Minute

// Pacer decides when each command is due: gaps of 60 s / rate with ±20 % jitter, and no
// commands inside the idle part of each cycle. Rate inside the active part is raised so the
// average over wall time is PerMin. Each pacer starts at a random point of its cycle, so a
// hundred readers do not go idle together.
type Pacer struct {
	gap    time.Duration // mean gap inside the active part
	idle   time.Duration // idle part of each period (0: never idle)
	origin time.Time     // start of a period, before start
	rng    *rand.Rand
	next   time.Time
}

// NewPacer paces p from start; seed makes the jitter reproducible.
func NewPacer(p Profile, start time.Time, seed uint64) *Pacer {
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	active := 1 - p.IdleFrac
	pc := &Pacer{
		gap:  time.Duration(float64(time.Minute) * active / p.PerMin),
		idle: time.Duration(float64(idlePeriod) * p.IdleFrac),
		rng:  rng,
	}
	pc.origin = start.Add(-time.Duration(rng.Int64N(int64(idlePeriod))))
	pc.next = pc.skipIdle(start.Add(pc.jitter()))
	return pc
}

func (pc *Pacer) jitter() time.Duration {
	return time.Duration(float64(pc.gap) * (0.8 + 0.4*pc.rng.Float64()))
}

// skipIdle moves t out of an idle window, to the start of the next period.
func (pc *Pacer) skipIdle(t time.Time) time.Time {
	if pc.idle == 0 {
		return t
	}
	into := t.Sub(pc.origin) % idlePeriod
	if into >= idlePeriod-pc.idle {
		return t.Add(idlePeriod - into)
	}
	return t
}

// Idle reports whether t falls in an idle window.
func (pc *Pacer) Idle(t time.Time) bool {
	return pc.idle > 0 && t.Sub(pc.origin)%idlePeriod >= idlePeriod-pc.idle
}

// Next returns when the next command is due and advances the pacer.
func (pc *Pacer) Next() time.Time {
	t := pc.next
	pc.next = pc.skipIdle(t.Add(pc.jitter()))
	return t
}
