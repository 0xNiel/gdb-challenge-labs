// Package perf is the load driver behind labd-perf (Phase 4): profiles, virtual users, the
// host collector, scenarios and the report (spec "Local performance test suite").
package perf

import (
	"math"
	"slices"
)

// Pctl is a distribution summary. Values are nearest-rank percentiles.
type Pctl struct {
	N   int     `json:"n"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Max float64 `json:"max"`
}

// Percentiles summarises xs (not modified). An empty input gives the zero Pctl.
func Percentiles(xs []float64) Pctl {
	if len(xs) == 0 {
		return Pctl{}
	}
	s := slices.Clone(xs)
	slices.Sort(s)
	return Pctl{N: len(s), P50: Pct(s, 50), P95: Pct(s, 95), P99: Pct(s, 99), Max: s[len(s)-1]}
}

// Pct is the nearest-rank p-th percentile of sorted values: the smallest value with at least
// p % of the values at or below it. p=100 is the maximum.
func Pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	return sorted[max(0, min(i, len(sorted)-1))]
}

// Mean is the arithmetic mean (0 for no values).
func Mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var t float64
	for _, x := range xs {
		t += x
	}
	return t / float64(len(xs))
}

// round rounds to d decimals, for readable JSON.
func round(x float64, d int) float64 {
	p := math.Pow(10, float64(d))
	return math.Round(x*p) / p
}
