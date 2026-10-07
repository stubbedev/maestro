// composer.json listeners that are not strings, which doDispatch takes for
// callables (#39).

package eventdispatcher

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// withScripts is createComposerInstance() whose root package has the
// scripts of the JSON object scriptsJSON.
func withScripts(t *testing.T, scriptsJSON string) *fakeComposer {
	t.Helper()
	c := createComposerInstance()
	v, err := php.JSONDecode(scriptsJSON, true)
	if err != nil {
		t.Fatal(err)
	}
	c.root.(*pkg.RootPackage).SetScripts(v.(*php.Array))

	return c
}

func TestDispatch_NonStringScriptListeners(t *testing.T) {
	const errorException, typeError, runtime = "ErrorException", "TypeError", "RuntimeException"
	for _, tc := range []struct {
		listener, class, message string
	}{
		{`42`, errorException, "Trying to access array offset on int"},
		{`1.5`, errorException, "Trying to access array offset on float"},
		{`true`, errorException, "Trying to access array offset on true"},
		{`false`, errorException, "Trying to access array offset on false"},
		{`null`, errorException, "Trying to access array offset on null"},
		{`{"a": "b"}`, errorException, "Undefined array key 0"},
		{`[1, "m"]`, typeError, "get_class(): Argument #1 ($object) must be of type object, int given"},
		{`["C"]`, errorException, "Undefined array key 1"},
		{`["C", ["m"]]`, errorException, "Array to string conversion"},
		{`["C", "m", "x"]`, runtime, `Subscriber C::m for event foo is not callable, make sure the function is defined and public`},
		{`["Vendor\\Missing", "m"]`, runtime, `Subscriber Vendor\Missing::m for event foo is not callable, make sure the function is defined and public`},
		{`["Vendor\\Exists", "other"]`, runtime, `Subscriber Vendor\Exists::other for event foo is not callable, make sure the function is defined and public`},
	} {
		t.Run(tc.listener, func(t *testing.T) {
			rt := newFakeRuntime()
			rt.methods[`Vendor\Exists::run`] = func(Event) (bool, error) { return false, nil }
			d := newDispatcher(t, withScripts(t, `{"foo": [`+tc.listener+`]}`), newRecordingIO(), processmock.New())
			d.SetScriptRuntime(rt)

			_, err := d.DispatchScript("foo", false, nil, nil)
			if err == nil {
				t.Fatal("no error")
			}
			if err.Error() != tc.message {
				t.Errorf("message %q, want %q", err.Error(), tc.message)
			}
			class := ""
			if e, ok := err.(phperr.Exception); ok { //nolint:errorlint // the thrown object itself
				class = e.PHPClass()
			}
			if class != tc.class {
				t.Errorf("class %q (%T), want %q", class, err, tc.class)
			}
		})
	}
}

func TestDispatch_ArrayScriptListenerIsCalled(t *testing.T) {
	var calls []string
	rt := newFakeRuntime()
	rt.methods[`Vendor\Exists::run`] = recorder(&calls, "run", false)
	out := bufferIO(t, console.VerbosityVerbose)
	d := newDispatcher(t, withScripts(t, `{"foo": [["Vendor\\Exists", "run"], ["Vendor\\Exists", "run"]]}`), out, processmock.New())
	d.SetScriptRuntime(rt)

	if _, err := d.DispatchScript("foo", false, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Errorf("calls %v", calls)
	}
	expectOutput(t, out, `> foo: Vendor\Exists->run`, `> foo: Vendor\Exists->run`)
}

func TestDispatch_NonStringListenerAfterStrings(t *testing.T) {
	// the listeners before it run: the error is the dispatch of the
	// listener, not a refusal of the list
	var calls []string
	rt := newFakeRuntime()
	rt.methods[`Vendor\Exists::run`] = recorder(&calls, "run", false)
	d := newDispatcher(t, withScripts(t, `{"foo": ["Vendor\\Exists::run", 42, "Vendor\\Exists::run"]}`), newRecordingIO(), processmock.New())
	d.SetScriptRuntime(rt)

	_, err := d.DispatchScript("foo", false, nil, nil)
	if e, ok := errors.AsType[*util.ErrorException](err); !ok || e.Message != "Trying to access array offset on int" {
		t.Fatalf("err = %v", err)
	}
	if len(calls) != 1 {
		t.Errorf("calls %v", calls)
	}
}
