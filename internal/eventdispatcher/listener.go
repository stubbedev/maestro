// Ports src/Composer/EventDispatcher/EventSubscriberInterface.php and the
// listener shapes EventDispatcher accepts (callable|string).

package eventdispatcher

// Listener is one entry of the listener table: a Script, a GoFunc or a
// PHPCallable.
type Listener interface {
	isListener()
}

// Script is a string listener: a composer.json "scripts" entry, or a
// string passed to addListener (a shell command, @php/@putenv/@composer
// line, @script reference, Class::method or command class).
type Script string

// GoFunc is a Go-internal listener (a Closure in Composer, such as
// RequireCommand's PRE_OPERATIONS_EXEC hook). Its return value is void.
type GoFunc func(ev Event) error

// PHPCallable is a non-string PHP callable registered from PHP (a plugin's
// [$object, 'method'], ['Class', 'method'], closure or invokable object),
// called through the ScriptRuntime.
type PHPCallable struct {
	// H is the PHP handle of the callable (for a closure or invokable, the
	// object's handle).
	H Handle
	// ObjectH is the handle of $object in an [$object, 'method'] callable;
	// 0 otherwise.
	ObjectH Handle
	// Class is get_class($callable[0]) or $callable[0] for an array
	// callable; "" otherwise.
	Class string
	// Method is $callable[1] for an array callable; "" otherwise.
	Method string
	// Closure is true for a \Closure.
	Closure bool
}

func (Script) isListener()      {}
func (GoFunc) isListener()      {}
func (PHPCallable) isListener() {}

// isArray reports whether c is an array callable [$objectOrClass, 'method'].
func (c PHPCallable) isArray() bool { return c.Method != "" }

// key is makeAutoloader's $callableKey for c.
func (c PHPCallable) key() string {
	switch {
	case c.isArray():
		return c.Class + "::" + c.Method
	case c.Closure:
		return "closure"
	}

	return "unknown"
}

// MatchCallable matches the PHP callable h (removeListener($callable):
// `$listener === $candidate`).
func MatchCallable(h Handle) func(Listener) bool {
	return func(l Listener) bool {
		c, ok := l.(PHPCallable)

		return ok && c.H == h
	}
}

// MatchObject matches the array callables whose object is h
// (removeListener($object): `$candidate[0] === $listener`) and the
// closure/invokable h itself.
func MatchObject(h Handle) func(Listener) bool {
	return func(l Listener) bool {
		c, ok := l.(PHPCallable)

		return ok && (c.H == h || (c.isArray() && c.ObjectH == h))
	}
}

// MatchScript matches the string listener s (removeListener($string)).
func MatchScript(s string) func(Listener) bool {
	return func(l Listener) bool {
		v, ok := l.(Script)

		return ok && string(v) == s
	}
}

// Subscription is one listener of an EventSubscriber.
type Subscription struct {
	Event    string
	Listener Listener
	Priority int
}

// EventSubscriber is Composer\EventDispatcher\EventSubscriberInterface for
// Go subscribers: SubscribedEvents is getSubscribedEvents() with its
// method-name shapes already resolved to listeners, in order. PHP
// subscribers are expanded in PHP (docs/PLUGINS.md §5.5).
type EventSubscriber interface {
	SubscribedEvents() []Subscription
}
