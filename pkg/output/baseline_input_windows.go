//go:build windows

package output

import (
	"time"

	"golang.org/x/sys/windows"
)

func baselineWaitForInput(fd int, timeout time.Duration) (bool, error) {
	timeoutMilliseconds := uint32(0)
	if timeout > 0 {
		timeoutMilliseconds = uint32((timeout + time.Millisecond - 1) / time.Millisecond)
	}
	event, err := windows.WaitForSingleObject(windows.Handle(fd), timeoutMilliseconds)
	if err != nil {
		return false, err
	}
	return event == windows.WAIT_OBJECT_0, nil
}
