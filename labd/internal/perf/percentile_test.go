package perf

import "testing"

func TestPercentiles(t *testing.T) {
	t.Parallel()
	seq := func(n int) []float64 {
		xs := make([]float64, n)
		for i := range xs {
			xs[i] = float64(n - i) // reversed: Percentiles must sort
		}
		return xs
	}
	cases := []struct {
		name string
		in   []float64
		want Pctl
	}{
		{"empty", nil, Pctl{}},
		{"one", []float64{7}, Pctl{N: 1, P50: 7, P95: 7, P99: 7, Max: 7}},
		{"1..100", seq(100), Pctl{N: 100, P50: 50, P95: 95, P99: 99, Max: 100}},
		{"1..10", seq(10), Pctl{N: 10, P50: 5, P95: 10, P99: 10, Max: 10}},
		{"1..1000", seq(1000), Pctl{N: 1000, P50: 500, P95: 950, P99: 990, Max: 1000}},
		{"ties", []float64{1, 1, 1, 9}, Pctl{N: 4, P50: 1, P95: 9, P99: 9, Max: 9}},
	}
	for _, c := range cases {
		if got := Percentiles(c.in); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
	in := []float64{3, 1, 2}
	Percentiles(in)
	if in[0] != 3 {
		t.Error("Percentiles sorted its input")
	}
	if m := Mean([]float64{1, 2, 3, 6}); m != 3 {
		t.Errorf("Mean %v", m)
	}
}
