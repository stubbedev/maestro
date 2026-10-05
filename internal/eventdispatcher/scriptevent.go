// Ports src/Composer/Script/Event.php.

package eventdispatcher

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// ScriptEvent is Composer\Script\Event, the event of the root package's
// scripts. It is here rather than in internal/script because it extends
// the base Event and EventDispatcher creates it.
type ScriptEvent struct {
	BaseEvent
	composer         Composer
	io               io.IO
	devMode          bool
	originatingEvent Event
}

// NewScriptEvent is new Script\Event($name, $composer, $io, $devMode,
// $args, $flags); a nil flags is [].
func NewScriptEvent(name string, composer Composer, io io.IO, devMode bool, args []string, flags *php.Array) *ScriptEvent {
	e := &ScriptEvent{composer: composer, io: io, devMode: devMode}
	e.init(name, args, flags)

	return e
}

// Class implements Event.
func (*ScriptEvent) Class() string { return `Composer\Script\Event` }

// Composer is getComposer().
func (e *ScriptEvent) Composer() Composer { return e.composer }

// IO is getIO().
func (e *ScriptEvent) IO() io.IO { return e.io }

// IsDevMode is isDevMode().
func (e *ScriptEvent) IsDevMode() bool { return e.devMode }

// OriginatingEvent is getOriginatingEvent(): the event that caused this
// one to be dispatched (through an @script reference), nil for none.
func (e *ScriptEvent) OriginatingEvent() Event { return e.originatingEvent }

// SetOriginatingEvent is setOriginatingEvent(): it records the upper-most
// originating event, skipping intermediate script events.
func (e *ScriptEvent) SetOriginatingEvent(event Event) *ScriptEvent {
	e.originatingEvent = calculateOriginatingEvent(event)
	e.bump()

	return e
}

func calculateOriginatingEvent(event Event) Event {
	if se, ok := event.(*ScriptEvent); ok && se.OriginatingEvent() != nil {
		return calculateOriginatingEvent(se.OriginatingEvent())
	}

	return event
}
