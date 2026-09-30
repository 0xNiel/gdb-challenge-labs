package perf

import (
	"strings"
	"testing"
	"time"
)

func loadRealScript(t *testing.T) Script {
	t.Helper()
	s, err := LoadScript("../../../images/perf/session.gdb")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestParseScript(t *testing.T) {
	t.Parallel()
	s, err := ParseScript(strings.NewReader("# intro\n\n# profile: a\nx\n  y  \n# comment\n# profile: b\nz\n"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(s.Order, ",") != "a,b" || strings.Join(s.Sections["a"], ",") != "x,y" || s.Sections["b"][0] != "z" {
		t.Fatalf("%+v", s)
	}
	if _, err := ParseScript(strings.NewReader("x\n# profile: a\n")); err == nil {
		t.Error("a command before the first tag was accepted")
	}
	if _, err := ParseScript(strings.NewReader("# profile: a\nx\n# profile: a\ny\n")); err == nil {
		t.Error("a repeated section was accepted")
	}
}

func TestProfiles_FromSessionGdb(t *testing.T) {
	t.Parallel()
	ps, err := Profiles(loadRealScript(t))
	if err != nil {
		t.Fatal(err)
	}
	r, st, ab := ps[Reader], ps[Stepper], ps[Abuser]
	if r.Loop[0] != "list" || !contains(r.Setup, "run") || !contains(r.Setup, "set confirm off") {
		t.Errorf("reader %+v", r)
	}
	if !contains(st.Loop, "next") || !contains(st.Loop, "step") || !contains(st.Loop, "watch counter") || contains(st.Loop, "info locals") {
		t.Errorf("stepper loop %v", st.Loop)
	}
	if !ab.Abuse || len(ab.Loop) != 0 {
		t.Errorf("abuser %+v", ab)
	}
	for _, p := range ps {
		for _, c := range append(p.Setup, p.Loop...) {
			for _, bad := range []string{"shell", "python", "pipe"} {
				if strings.HasPrefix(c, bad) {
					t.Errorf("%s sends %q", p.Name, c)
				}
			}
		}
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// Simulated 10 minutes: the rate over wall time, the jitter and the idle windows.
func TestPacer_Rates(t *testing.T) {
	t.Parallel()
	ps, _ := Profiles(loadRealScript(t))
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		profile  string
		min, max int
	}{{Reader, 15, 25}, {Stepper, 185, 215}} {
		for seed := uint64(1); seed <= 20; seed++ {
			pc := NewPacer(ps[c.profile], start, seed)
			n := 0
			var prev time.Time
			for tt := pc.Next(); tt.Before(start.Add(10 * time.Minute)); tt = pc.Next() {
				if pc.Idle(tt) {
					t.Fatalf("%s seed %d: command at %v inside an idle window", c.profile, seed, tt.Sub(start))
				}
				if !prev.IsZero() && tt.Sub(prev) < time.Duration(float64(pc.gap)*0.8) {
					t.Fatalf("%s: gap %v below the jitter floor", c.profile, tt.Sub(prev))
				}
				prev = tt
				n++
			}
			if n < c.min || n > c.max {
				t.Errorf("%s seed %d: %d commands in 10 min, want %d..%d", c.profile, seed, n, c.min, c.max)
			}
		}
	}
	// Averaged over many readers the rate is 2/min.
	total := 0
	for seed := uint64(100); seed < 300; seed++ {
		pc := NewPacer(ps[Reader], start, seed)
		for tt := pc.Next(); tt.Before(start.Add(10 * time.Minute)); tt = pc.Next() {
			total++
		}
	}
	if avg := float64(total) / 200 / 10; avg < 1.85 || avg > 2.15 {
		t.Errorf("reader average %.2f commands/min, want about 2", avg)
	}
}
