//go:build unix

package php

import "golang.org/x/sys/unix"

// getwd is getcwd(3), the physical directory PHP's getcwd() returns.
func getwd() (string, error) { return unix.Getwd() }
