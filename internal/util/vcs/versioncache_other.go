//go:build !linux

// Ports nothing: git's version is kept across runs only on Linux (see
// UseVersionCache).

package vcs

import (
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

func gitBinaryKey(string, time.Time, fsstate.Margin) string { return "" }
