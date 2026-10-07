// Ports the gc_collect_cycles(); gc_disable() at the start of Composer's
// Installer::run and its gc_enable() after the post-install/update event:
// the state of PHP's cycle collector in Composer's process, which the
// plugin runtime gives its php (a Composer static of the sync engine).

package util

import "sync/atomic"

// phpGC is that state: 0 as PHP's ini left it, 1 disabled, 2 enabled.
var phpGC atomic.Int32

// SetPHPGC records gc_disable() (enabled false, after gc_collect_cycles())
// or gc_enable().
func SetPHPGC(enabled bool) {
	if enabled {
		phpGC.Store(2)
	} else {
		phpGC.Store(1)
	}
}

// PHPGC returns the state of PHP's cycle collector: touched is false
// while no Installer ran, the collector then being as PHP's ini has it.
func PHPGC() (enabled, touched bool) {
	v := phpGC.Load()

	return v == 2, v != 0
}
