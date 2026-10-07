// Ports src/Composer/EventDispatcher/Event.php.

package eventdispatcher

import (
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// Event is Composer\EventDispatcher\Event and its subclasses as the
// dispatcher sees them.
//
// Rev is a change counter bumped by every setter, so the plugin bridge can
// tell which event mirrors to resync (docs/PLUGINS.md §5.3). Class is the
// PHP class of the event.
type Event interface {
	Name() string
	// Arguments is getArguments(): the additional arguments passed by the
	// user (strings, as they come from the command line).
	Arguments() []string
	// Flags is getFlags(), an array<string, mixed>; never nil.
	Flags() *php.Array
	IsPropagationStopped() bool
	StopPropagation()
	Rev() uint64
	PHPClass() string
}

// ComposerEvent is an event carrying the Composer instance, the IO and the
// dev mode: Script\Event, Installer\PackageEvent and InstallerEvent, the
// classes EventDispatcher checks for with instanceof.
type ComposerEvent interface {
	Event
	Composer() Composer
	IO() io.IO
	IsDevMode() bool
}

// BaseEvent is Composer\EventDispatcher\Event. The typed events embed it.
type BaseEvent struct {
	name               string
	args               []string
	flags              *php.Array
	propagationStopped bool
	rev                uint64
}

// NewEvent is new Event($name, $args, $flags); a nil flags is [].
func NewEvent(name string, args []string, flags *php.Array) *BaseEvent {
	e := &BaseEvent{}
	e.init(name, args, flags)

	return e
}

func (e *BaseEvent) init(name string, args []string, flags *php.Array) {
	if flags == nil {
		flags = php.NewArray()
	}

	e.name, e.args, e.flags = name, args, flags
}

// Name is getName().
func (e *BaseEvent) Name() string { return e.name }

// Arguments is getArguments().
func (e *BaseEvent) Arguments() []string { return e.args }

// Flags is getFlags().
func (e *BaseEvent) Flags() *php.Array { return e.flags }

// IsPropagationStopped is isPropagationStopped().
func (e *BaseEvent) IsPropagationStopped() bool { return e.propagationStopped }

// StopPropagation is stopPropagation(): no further listener is called.
func (e *BaseEvent) StopPropagation() {
	e.propagationStopped = true
	e.rev++
}

// Rev implements Event.
func (e *BaseEvent) Rev() uint64 { return e.rev }

// PHPClass implements Event.
func (*BaseEvent) PHPClass() string { return `Composer\EventDispatcher\Event` }

// bump records a change made by a subclass setter.
func (e *BaseEvent) bump() { e.rev++ }

// Handle identifies an object owned by the PHP side of the plugin runtime
// (docs/PLUGINS.md §5.3).
type Handle int64

// EventContext is what a PHP-owned event inherits from Script\Event,
// PackageEvent or InstallerEvent.
type EventContext struct {
	Composer Composer
	IO       io.IO
	DevMode  bool
}

// PHPEvent is an event object created in PHP (an Event subclass of a
// plugin, docs/PLUGINS.md §5.5), dispatched from Go by its handle. Its name,
// arguments and flags are fetched once by the plugin bridge; propagation
// changes come back through StopPropagation.
type PHPEvent struct {
	BaseEvent
	// H is the PHP handle of the event object.
	H Handle
	// class is the event's PHP class.
	class string
	// Context is non-nil when the class extends Script\Event, PackageEvent or
	// InstallerEvent.
	Context *EventContext
}

// NewPHPEvent returns the Go side of the PHP event object h.
func NewPHPEvent(h Handle, class, name string, args []string, flags *php.Array, ctx *EventContext) *PHPEvent {
	e := &PHPEvent{H: h, class: class, Context: ctx}
	e.init(name, args, flags)

	return e
}

// PHPClass implements Event.
func (e *PHPEvent) PHPClass() string { return e.class }

// composerContext is the Composer, IO and dev mode of an event that has
// them (`$event instanceof ScriptEvent || PackageEvent || InstallerEvent`).
func composerContext(ev Event) (EventContext, bool) {
	switch e := ev.(type) {
	case ComposerEvent:
		return EventContext{Composer: e.Composer(), IO: e.IO(), DevMode: e.IsDevMode()}, true
	case *PHPEvent:
		if e.Context != nil {
			return *e.Context, true
		}
	}

	return EventContext{}, false
}
