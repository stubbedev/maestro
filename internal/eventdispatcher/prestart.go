// Ports nothing: starting the script runtime ahead of the dispatch that
// needs it, so that PHP's start overlaps the work before (Composer runs
// in PHP, which is running by then).

package eventdispatcher

import "strings"

// Prestarter is a ScriptRuntime that can start ahead of its first need
// (plugin.Runtime.Prestart): in the background, its first use waiting for
// that start.
type Prestarter interface {
	Prestart()
}

// Expect tells dispatcher that the caller goes on to dispatch events (by
// name). When a root script of one of them runs in PHP (a Class::method
// that is not native or a command class, or a script it references with
// @), the script runtime starts now, so its start overlaps the caller's
// work. A dispatcher that is not an *EventDispatcher, or runs no scripts,
// starts nothing.
func Expect(dispatcher any, events ...string) {
	d, ok := dispatcher.(*EventDispatcher)
	if !ok || !d.runScripts {
		return
	}
	p, ok := d.runtime.(Prestarter)
	if !ok {
		return
	}

	seen := map[string]bool{}
	for _, name := range events {
		if d.scriptsRunPHP(name, seen) {
			p.Prestart()

			return
		}
	}
}

// scriptsRunPHP reports whether a root script of the event name runs in
// PHP; seen holds the events already looked at.
func (d *EventDispatcher) scriptsRunPHP(name string, seen map[string]bool) bool {
	if seen[name] {
		return false
	}
	seen[name] = true

	for _, l := range d.getScriptListeners(NewEvent(name, nil, nil), false) {
		s, ok := l.(Script)
		if !ok {
			continue
		}
		callable := string(s)
		switch {
		case isComposerScript(callable):
			if ref, _, _ := strings.Cut(callable[1:], " "); ref != "composer" && d.scriptsRunPHP(ref, seen) {
				return true
			}
		case isPhpScript(callable):
			className, methodName, _ := strings.Cut(callable, "::")
			if nativeScript(className, methodName) == nil {
				return true
			}
		case isCommandClass(callable):
			return true
		}
	}

	return false
}
