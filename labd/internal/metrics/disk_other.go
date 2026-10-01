//go:build !linux

package metrics

// diskUsedGB is not measured off Linux (labd runs on Linux; unit tests also run on macOS).
func diskUsedGB(string) (float64, bool) { return 0, false }
