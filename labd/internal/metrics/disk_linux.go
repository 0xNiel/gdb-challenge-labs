//go:build linux

package metrics

import "syscall"

// diskUsedGB is the used space of the filesystem holding path, in GB (10^9 bytes).
func diskUsedGB(path string) (float64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, false
	}
	used := (st.Blocks - st.Bfree) * uint64(st.Bsize)
	return Round(float64(used)/1e9, 2), true
}
