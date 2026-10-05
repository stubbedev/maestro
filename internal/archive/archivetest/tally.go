package archivetest

import (
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/archive"
)

// Tally counts how the cases of a differential test came out.
type Tally struct {
	Same, BothFail, Refused int
}

// Compare checks maestro's result for one archive (its tree, or the error
// it refused the archive with) against the real extractor's.
func (c *Tally) Compare(t *testing.T, name string, real Result, got Tree, err error, mayRefuse bool) {
	t.Helper()

	if err != nil {
		if _, ok := errors.AsType[*archive.Error](err); !ok {
			t.Errorf("%s: unexpected error type %T: %v", name, err, err)
			return
		}
	}

	switch {
	case real.OK() && err == nil:
		if d := Diff(real.Tree, got); len(d) > 0 {
			t.Errorf("%s: trees differ:\n%s", name, Short(d))
			return
		}

		c.Same++
	case !real.OK() && err != nil:
		c.BothFail++
	case real.OK():
		if !mayRefuse {
			t.Errorf("%s: maestro refused what the extractor handles: %v", name, err)
			return
		}

		c.Refused++
		t.Logf("%s: refused: %v", name, err)
	default:
		t.Errorf("%s: the extractor failed (exit %d: %s) but maestro extracted %d entries", name, real.Exit, strings.TrimSpace(real.Output), len(got))
	}
}

// Log reports the tally.
func (c *Tally) Log(t *testing.T, what string) {
	t.Helper()
	t.Logf("%s: %d identical, %d refused by both, %d refused by maestro only", what, c.Same, c.BothFail, c.Refused)
}
