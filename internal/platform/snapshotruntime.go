package platform

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
)

// SnapshotRuntime is the Runtime of a probed php: it answers every call
// from the Snapshot. Calls the probe did not record (Invoke and Construct
// of callables probe.php does not list, constants of classes it does not
// list) are programming errors: they return a *NotProbedError, or panic
// with one where the method cannot return an error.
type SnapshotRuntime struct {
	s *Snapshot
}

var _ Runtime = (*SnapshotRuntime)(nil)

// NewRuntime returns the Runtime answering from s.
func NewRuntime(s *Snapshot) *SnapshotRuntime { return &SnapshotRuntime{s: s} }

// NotProbedError is a Runtime question the probe has no answer for. Add
// the call to probe.php.
type NotProbedError struct {
	Call string
}

func (e *NotProbedError) Error() string {
	return "platform: " + e.Call + " is not recorded by the probe; add it to internal/platform/probe.php"
}

// HasConstant implements Runtime.
func (r *SnapshotRuntime) HasConstant(constant, class string) bool {
	_, err := r.GetConstant(constant, class)
	if np, ok := err.(*NotProbedError); ok { //nolint:errorlint // never wrapped
		panic(np)
	}

	return err == nil
}

// GetConstant implements Runtime.
func (r *SnapshotRuntime) GetConstant(constant, class string) (any, error) {
	// constant(ltrim($class.'::'.$constant, ':'))
	name := strings.TrimLeft(class+"::"+constant, ":")

	if i := strings.Index(name, "::"); i >= 0 {
		return r.classConstant(name[:i], name[i+2:])
	}

	name = strings.TrimPrefix(name, `\`)

	if v, ok := r.s.constants[name]; ok {
		return v, nil
	}

	// true, false and null are case-insensitive.
	switch php.Strtolower(name) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}

	return nil, &PHPError{Class: "Error", Message: `Undefined constant "` + name + `"`, Site: constantSite}
}

func (r *SnapshotRuntime) classConstant(class, constant string) (any, error) {
	class = strings.TrimPrefix(class, `\`)
	if !r.HasClass(class) {
		return nil, &PHPError{Class: "Error", Message: `Class "` + class + `" not found`, Site: constantSite}
	}

	for k, v := range r.s.classConstants.All() {
		if !strings.EqualFold(k.String(), class) {
			continue
		}

		consts, _ := v.(*php.Array)
		if c, ok := consts.Get(constant); ok {
			return unwrap(c), nil
		}

		return nil, &PHPError{Class: "Error", Message: "Undefined constant " + k.String() + "::" + constant, Site: constantSite}
	}

	return nil, &NotProbedError{Call: "constant(" + class + "::" + constant + ")"}
}

// HasFunction implements Runtime.
func (r *SnapshotRuntime) HasFunction(fn string) bool {
	_, ok := r.s.functions[php.Strtolower(strings.TrimPrefix(fn, `\`))]

	return ok
}

// HasClass implements Runtime.
func (r *SnapshotRuntime) HasClass(class string) bool {
	_, ok := r.s.classes[php.Strtolower(strings.TrimPrefix(class, `\`))]

	return ok
}

// Invoke implements Runtime.
func (r *SnapshotRuntime) Invoke(callable Callable, arguments ...any) (any, error) {
	// Runtime::invoke(callable $callable, ...) rejects what is not
	// callable; PHP adds where it was called from to the message.
	if callable.Class == "" && !r.HasFunction(callable.Name) {
		return nil, notCallable("string")
	}

	if callable.Class != "" && !r.HasClass(callable.Class) {
		return nil, notCallable("array")
	}

	key := php.Strtolower(strings.TrimPrefix(callable.String(), `\`))

	return findCall(r.s.calls, key, arguments, callable.String(), phperr.At("Runtime.php", 49))
}

// Construct implements Runtime.
func (r *SnapshotRuntime) Construct(class string, arguments ...any) (any, error) {
	class = strings.TrimPrefix(class, `\`)
	// new $class (line 72) without arguments, else ReflectionClass (75)
	// and newInstanceArgs (77).
	site := phperr.At("Runtime.php", 72)
	if len(arguments) > 0 {
		site.Line = 75
	}
	if !r.HasClass(class) {
		return nil, &PHPError{Class: "Error", Message: `Class "` + class + `" not found`, Site: site}
	}
	if len(arguments) > 0 {
		site.Line = 77
	}

	return findCall(r.s.calls, "new "+php.Strtolower(class), arguments, "new "+class, site)
}

// constantSite is Runtime::getConstant's constant() call.
var constantSite = phperr.At("Runtime.php", 34)

// findCall returns the recorded outcome of the call key(arguments...).
// A recorded error gets site, where Runtime makes the call, when known.
func findCall(calls []probeCall, key string, arguments []any, display string, site phperr.Site) (any, error) {
	for i := range calls {
		c := &calls[i]
		if c.callable != key || !sameArguments(c.args, arguments) {
			continue
		}

		if c.err != nil {
			if site.Known() {
				e := *c.err
				e.Site = site

				return nil, &e
			}

			return nil, c.err
		}

		return c.value, nil
	}

	return nil, &NotProbedError{Call: display + "(" + describeArguments(arguments) + ")"}
}

func sameArguments(recorded, arguments []any) bool {
	if len(recorded) != len(arguments) {
		return false
	}

	for i, a := range arguments {
		if !php.StrictEquals(recorded[i], normalizeArgument(a)) {
			return false
		}
	}

	return true
}

// normalizeArgument converts Go ints to PHP's int64.
func normalizeArgument(a any) any {
	if i, ok := a.(int); ok {
		return int64(i)
	}

	return a
}

func describeArguments(arguments []any) string {
	parts := make([]string, len(arguments))
	for i, a := range arguments {
		parts[i] = php.VarExport(normalizeArgument(a))
	}

	return strings.Join(parts, ", ")
}

// GetExtensions implements Runtime.
func (r *SnapshotRuntime) GetExtensions() []string {
	names := make([]string, len(r.s.Extensions))
	for i := range r.s.Extensions {
		names[i] = r.s.Extensions[i].Name
	}

	return names
}

// extension finds a loaded extension; PHP looks names up case-insensitively.
func (r *SnapshotRuntime) extension(name string) (*Extension, bool) {
	i, ok := r.s.extIndex[php.Strtolower(name)]
	if !ok {
		return nil, false
	}

	return &r.s.Extensions[i], true
}

// GetExtensionVersion implements Runtime.
func (r *SnapshotRuntime) GetExtensionVersion(extension string) string {
	if ext, ok := r.extension(extension); ok && ext.VersionOK {
		return ext.Version
	}

	return "0"
}

// GetExtensionInfo implements Runtime.
func (r *SnapshotRuntime) GetExtensionInfo(extension string) (string, error) {
	ext, ok := r.extension(extension)
	if !ok {
		return "", &PHPError{Class: "ReflectionException", Message: `Extension "` + extension + `" does not exist`, Site: phperr.At("Runtime.php", 101)}
	}

	return ext.Info, nil
}

// probedObject is an object a probed call returned, answering the method
// calls recorded for it. It implements ResourceBundle and Imagick.
type probedObject struct {
	class   string
	methods []probeCall
}

// Class is the object's class name.
func (o *probedObject) Class() string { return o.class }

// Call returns the recorded outcome of $object->method(...$arguments).
func (o *probedObject) Call(method string, arguments ...any) (any, error) {
	return findCall(o.methods, php.Strtolower(method), arguments, o.class+"::"+method, phperr.Site{})
}

// Get implements ResourceBundle.
func (o *probedObject) Get(field any) (any, error) { return o.Call("get", field) }

// GetVersion implements Imagick.
func (o *probedObject) GetVersion() (any, error) { return o.Call("getVersion") }

// notCallable is the TypeError of Runtime::invoke's callable parameter.
func notCallable(given string) *PHPError {
	return &PHPError{Class: "TypeError", Message: `Composer\Platform\Runtime::invoke(): Argument #1 ($callable) must be of type callable, ` + given + " given", Site: phperr.At("Runtime.php", 47)}
}
