//go:build !linux

// Ports nothing: guesses are kept across runs only on Linux (see
// UseGuessCache).

package version

import (
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

func repoState(*fsstate.KeyHash, string, time.Time, fsstate.Margin) (trusted, ok bool) {
	return false, false
}
