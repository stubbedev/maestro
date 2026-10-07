// Ports nothing: Installer::run's script events announced to the
// dispatcher ahead of their dispatch (eventdispatcher.Expect).

package composer

import (
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/script"
)

// expectScripts tells the event dispatcher which script events the run
// dispatches for sure: the command's pre and post events and, when the
// autoloader is dumped, the dump's. Package events depend on the
// operations and are left out.
func (i *Installer) expectScripts() {
	events := []string{script.PreInstallCmd, script.PostInstallCmd}
	if i.update {
		events = []string{script.PreUpdateCmd, script.PostUpdateCmd}
	}
	if i.dumpAutoloader {
		events = append(events, script.PreAutoloadDump, script.PostAutoloadDump)
	}
	eventdispatcher.Expect(i.eventDispatcher, events...)
}
