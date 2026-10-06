// EventDispatcher's protected methods, for the plugin shim: what a
// subclass written in PHP calls on itself (parent::doDispatch(),
// $this->getListeners(), ...) runs maestro's dispatcher's code.

package eventdispatcher

// DoDispatch is the protected doDispatch($event): the listeners of the
// event run, the highest return value.
func (d *EventDispatcher) DoDispatch(event Event) (int, error) { return d.doDispatch(event) }

// ExecuteTty is the protected executeTty($exec).
func (d *EventDispatcher) ExecuteTty(exec string) (int, error) { return d.executeTty(exec) }

// PhpExecCommand is the protected getPhpExecCommand().
func (d *EventDispatcher) PhpExecCommand() (string, error) { return d.getPhpExecCommand() }

// EchoPhpScript is the line executeEventPhpScript() writes before calling
// $className::$methodName($event).
func (d *EventDispatcher) EchoPhpScript(event Event, className, methodName string) {
	d.echoPhpScript(event, className, methodName)
}

// Listeners is the protected getListeners($event): the listeners of the
// event, by priority, the scripts of the root package included.
func (d *EventDispatcher) Listeners(event Event) []Listener { return d.getListeners(event) }

// ScriptListeners is the protected getScriptListeners($event): the root
// package's scripts for the event.
func (d *EventDispatcher) ScriptListeners(event Event) []Listener {
	return d.getScriptListeners(event, true)
}

// PushEvent is the protected pushEvent($event): the size of the event
// stack after the push.
func (d *EventDispatcher) PushEvent(event Event) (int, error) {
	if err := d.pushEvent(event); err != nil {
		return 0, err
	}

	return len(d.eventStack), nil
}

// PopEvent is the protected popEvent(): the name of the event popped;
// false when the stack was empty (PHP's null).
func (d *EventDispatcher) PopEvent() (string, bool) {
	if len(d.eventStack) == 0 {
		return "", false
	}
	name := d.eventStack[len(d.eventStack)-1]
	d.popEvent()

	return name, true
}
