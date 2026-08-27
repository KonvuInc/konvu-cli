//go:build aix || darwin || dragonfly || freebsd || illumos || linux || netbsd || openbsd || solaris || zos

package output

import (
	"time"

	"golang.org/x/sys/unix"
)

func baselineWaitForInput(fd int, timeout time.Duration) (bool, error) {
	timeoutMilliseconds := int(timeout / time.Millisecond)
	if timeout > 0 && timeout%time.Millisecond != 0 {
		timeoutMilliseconds++
	}
	if timeoutMilliseconds < 0 {
		timeoutMilliseconds = 0
	}

	input := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	for {
		ready, err := unix.Poll(input, timeoutMilliseconds)
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		if ready == 0 {
			return false, nil
		}
		if input[0].Revents&unix.POLLNVAL != 0 {
			return false, unix.EBADF
		}
		return input[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) != 0, nil
	}
}
