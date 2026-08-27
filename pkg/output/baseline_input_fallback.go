//go:build !aix && !darwin && !dragonfly && !freebsd && !illumos && !linux && !netbsd && !openbsd && !solaris && !windows && !zos

package output

import "time"

func baselineWaitForInput(_ int, timeout time.Duration) (bool, error) {
	if timeout > 0 {
		time.Sleep(timeout)
	}
	return false, nil
}
