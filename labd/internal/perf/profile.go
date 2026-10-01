package perf

import (
	"bufio"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"slices"
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
	// Learner works a real lab: its user's challenge picks an episode, which it repeats from a
	// shell, starting and quitting gdb (Phase 7, task 7.10). Setup and Loop are unused.
	Learner  bool
	Episodes map[string]Episode // by challenge slug
}

// Episode is what a learner does in one lab (labd/perf/learner.txt).
type Episode struct {
	Slug, Entry string
	Steps       []Step
}

// Step is one line typed. AtShell: typed at the shell prompt. GDB: gdb's prompt comes back
// afterwards (otherwise the shell's).
type Step struct {
	Cmd     string
	AtShell bool
	GDB     bool
}

// Verb names a step in the command timings: shell_run, gdb_start, gdb_quit, or gdb's own
// command word (run, next, watch, continue, ...).
func (st Step) Verb() string {
	switch {
	case st.AtShell && st.GDB:
		return "gdb_start"
	case st.AtShell:
		return "shell_run"
	case !st.GDB:
		return "gdb_quit"
	}
	verb, _, _ := strings.Cut(st.Cmd, " ")
	return verb
}

// ParseLearner reads learner.txt: "== <slug> <entry>" starts a lab, "$ cmd" is a shell line,
// anything else a gdb line. After "$ gdb ..." gdb prompts; after "quit" or another shell line,
// the shell does. Every episode must start and end at the shell.
func ParseLearner(r io.Reader) (map[string]Episode, error) {
	eps := map[string]Episode{}
	var cur *Episode
	inGDB := false
	finish := func() error {
		if cur == nil {
			return nil
		}
		if len(cur.Steps) == 0 || inGDB {
			return fmt.Errorf("episode %s must have steps and end at the shell (quit gdb)", cur.Slug)
		}
		eps[cur.Slug] = *cur
		return nil
	}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if rest, ok := strings.CutPrefix(l, "=="); ok {
			if err := finish(); err != nil {
				return nil, err
			}
			f := strings.Fields(rest)
			if len(f) != 2 {
				return nil, fmt.Errorf("want \"== <slug> <entry>\", got %q", l)
			}
			if _, dup := eps[f[0]]; dup {
				return nil, fmt.Errorf("lab %s appears twice", f[0])
			}
			cur, inGDB = &Episode{Slug: f[0], Entry: f[1]}, false
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("line %q before the first \"== <slug> <entry>\"", l)
		}
		var st Step
		if cmd, ok := strings.CutPrefix(l, "$ "); ok {
			if inGDB {
				return nil, fmt.Errorf("%s: shell line %q while gdb is running (quit first)", cur.Slug, cmd)
			}
			st = Step{Cmd: cmd, AtShell: true, GDB: strings.HasPrefix(cmd, "gdb ")}
		} else {
			if !inGDB {
				return nil, fmt.Errorf("%s: gdb line %q at the shell (start gdb first)", cur.Slug, l)
			}
			st = Step{Cmd: l, GDB: l != "quit"}
		}
		inGDB = st.GDB
		cur.Steps = append(cur.Steps, st)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if err := finish(); err != nil {
		return nil, err
	}
	if len(eps) == 0 {
		return nil, fmt.Errorf("no episodes")
	}
	return eps, nil
}

// LoadLearner parses the file at path.
func LoadLearner(path string) (map[string]Episode, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	eps, err := ParseLearner(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return eps, nil
}

// LearnerProfile: a person working through a lab, about 6 commands a minute with 20 % idle
// (thinking, reading the lesson). ADR 0017.
func LearnerProfile(eps map[string]Episode) Profile {
	return Profile{Name: Learner, PerMin: 6, IdleFrac: 0.2, Learner: true, Episodes: eps}
}

// LearnerSlugs are the labs learners work, sorted, for round-robin assignment.
func LearnerSlugs(eps map[string]Episode) []string {
	out := make([]string, 0, len(eps))
	for s := range eps {
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

// Profile names.
const (
	Reader  = "reader"
	Stepper = "stepper"
	Abuser  = "abuser"
	Learner = "learner" // Phase 7, task 7.10
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
