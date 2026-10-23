//go:build !linux

// Ports nothing: git's version is kept across runs only on Linux (see
// UseVersionCache), and so is anything else keyed on the git binary
// (KeyGitBinary).

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

func gitBinaryKey(string, time.Time, fsstate.Margin) string { return "" }

// KeyGitBinary adds the git binary a process would start to k; on this
// system what git answers is not kept, so it reports false.
func KeyGitBinary(*fsstate.KeyHash, time.Time, fsstate.Margin) bool { return false }
