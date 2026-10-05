//go:build unix

package config

import "golang.org/x/sys/unix"

// setUmask sets the process umask the oracle goldens were generated with.
func setUmask(mask int) func() {
	old := unix.Umask(mask)

	return func() { unix.Umask(old) }
}
