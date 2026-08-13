//go:build !windows

package rlimit

import "golang.org/x/sys/unix"

// Raise lifts the RLIMIT_NOFILE soft limit to the hard limit so spawned
// shells inherit a usable fd budget. Processes started by launchd get a
// soft limit of 256, which a busy daemon can exhaust.
func Raise() {
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &lim); err != nil {
		return
	}
	max := lim.Max
	if max > 1048576 {
		max = 1048576 // macOS rejects RLIM_INFINITY for RLIMIT_NOFILE
	}
	if lim.Cur >= max {
		return
	}
	lim.Cur = max
	_ = unix.Setrlimit(unix.RLIMIT_NOFILE, &lim)
}
