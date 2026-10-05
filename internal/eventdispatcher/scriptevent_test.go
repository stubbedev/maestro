// Ports tests/Composer/Test/Script/EventTest.php.

package eventdispatcher

import (
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/pkg"
)

// createScriptTestComposer is EventTest's createComposerInstance().
func createScriptTestComposer() Composer {
	return &fakeComposer{root: pkg.NewRootPackage("__root__", "1.0.0.0", "1.0.0"), config: config.New(true, ""), autoload: &fakeAutoloader{}}
}

func TestScriptEvent_EventSetsOriginatingEvent(t *testing.T) {
	ioi := newRecordingIO()
	composer := createScriptTestComposer()

	originatingEvent := NewEvent("originatingEvent", nil, nil)

	scriptEvent := NewScriptEvent("test", composer, ioi, true, nil, nil)

	if scriptEvent.OriginatingEvent() != nil {
		t.Error("originatingEvent is initialized as null")
	}

	scriptEvent.SetOriginatingEvent(originatingEvent)

	if scriptEvent.OriginatingEvent() != Event(originatingEvent) {
		t.Error("getOriginatingEvent() SHOULD return test event")
	}
}

func TestScriptEvent_EventCalculatesNestedOriginatingEvent(t *testing.T) {
	ioi := newRecordingIO()
	composer := createScriptTestComposer()

	originatingEvent := NewEvent("upperOriginatingEvent", nil, nil)
	intermediateEvent := NewScriptEvent("intermediate", composer, ioi, true, nil, nil)
	intermediateEvent.SetOriginatingEvent(originatingEvent)

	scriptEvent := NewScriptEvent("test", composer, ioi, true, nil, nil)
	scriptEvent.SetOriginatingEvent(intermediateEvent)

	if scriptEvent.OriginatingEvent() == Event(intermediateEvent) {
		t.Error("getOriginatingEvent() SHOULD NOT return intermediate events")
	}

	if scriptEvent.OriginatingEvent() != Event(originatingEvent) {
		t.Error("getOriginatingEvent() SHOULD return upper-most event")
	}
}
