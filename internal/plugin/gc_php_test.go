package plugin

import (
	"testing"

	"github.com/stubbedev/maestro/internal/util"
)

// PHP's cycle collector is off while maestro's Installer runs, as
// Installer::run's gc_disable() leaves it in Composer, and on again after
// its gc_enable().
func TestRuntime_GC(t *testing.T) {
	requirePHP(t)

	rt, _, _ := newTestRuntime(t)
	start(t, rt)

	for _, enabled := range []bool{false, true} {
		util.SetPHPGC(enabled)
		if got := evalPHP(t, rt, `return gc_enabled();`, nil); got != enabled {
			t.Errorf("after SetPHPGC(%v): gc_enabled() = %v", enabled, got)
		}
	}
}
