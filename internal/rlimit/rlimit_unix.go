//go:build !windows

package rlimit

import "golang.org/x/sys/unix"

func Raise() {
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_NOFILE, &lim); err != nil {
		return
	}
	max := lim.Max
	if max > 1048576 {
		max = 1048576
	}
	if lim.Cur >= max {
		return
	}
	lim.Cur = max
	_ = unix.Setrlimit(unix.RLIMIT_NOFILE, &lim)
}
