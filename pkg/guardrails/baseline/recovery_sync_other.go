//go:build !(aix || android || darwin || dragonfly || freebsd || illumos || ios || linux || netbsd || openbsd || solaris)

package baseline

import "os"

func syncRootDirectory(_ *os.Root) error {
	return nil
}
