// Package platformmock provides mocks of platform.Runtime and
// platform.HhvmVersionDetector shaped like the PHPUnit mocks Composer's
// tests build (getMockBuilder(Runtime::class)->getMock()), so that tests
// such as PlatformRepositoryTest port one to one.
package platformmock

import (
	"slices"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/platform"
)

// Runtime is a platform.Runtime whose methods call the matching *Func
// field. A nil field answers as a PHPUnit mock does for a method without
// expectations: the zero value of the PHP return type (false, null, "",
// []). Every call is recorded in Calls.
type Runtime struct {
	HasConstantFunc         func(constant, class string) bool
	GetConstantFunc         func(constant, class string) (any, error)
	HasFunctionFunc         func(fn string) bool
	InvokeFunc              func(callable platform.Callable, arguments []any) (any, error)
	HasClassFunc            func(class string) bool
	ConstructFunc           func(class string, arguments []any) (any, error)
	GetExtensionsFunc       func() []string
	GetExtensionVersionFunc func(extension string) string
	GetExtensionInfoFunc    func(extension string) (string, error)

	mu    sync.Mutex
	calls []Call
}

// Call is one recorded method call.
type Call struct {
	Method    string
	Arguments []any
}

var _ platform.Runtime = (*Runtime)(nil)

func (m *Runtime) record(method string, arguments ...any) {
	m.mu.Lock()
	m.calls = append(m.calls, Call{Method: method, Arguments: arguments})
	m.mu.Unlock()
}

// Calls returns the calls made so far.
func (m *Runtime) Calls() []Call {
	m.mu.Lock()
	defer m.mu.Unlock()

	return slices.Clone(m.calls)
}

// CallsTo returns the calls made to method (expects(self::once()) checks).
func (m *Runtime) CallsTo(method string) []Call {
	var out []Call

	for _, c := range m.Calls() {
		if c.Method == method {
			out = append(out, c)
		}
	}

	return out
}

// HasConstant implements platform.Runtime.
func (m *Runtime) HasConstant(constant, class string) bool {
	m.record("hasConstant", constant, class)

	if m.HasConstantFunc == nil {
		return false
	}

	return m.HasConstantFunc(constant, class)
}

// GetConstant implements platform.Runtime.
func (m *Runtime) GetConstant(constant, class string) (any, error) {
	m.record("getConstant", constant, class)

	if m.GetConstantFunc == nil {
		return nil, nil
	}

	return m.GetConstantFunc(constant, class)
}

// HasFunction implements platform.Runtime.
func (m *Runtime) HasFunction(fn string) bool {
	m.record("hasFunction", fn)

	if m.HasFunctionFunc == nil {
		return false
	}

	return m.HasFunctionFunc(fn)
}

// Invoke implements platform.Runtime.
func (m *Runtime) Invoke(callable platform.Callable, arguments ...any) (any, error) {
	m.record("invoke", callable, arguments)

	if m.InvokeFunc == nil {
		return nil, nil
	}

	return m.InvokeFunc(callable, arguments)
}

// HasClass implements platform.Runtime.
func (m *Runtime) HasClass(class string) bool {
	m.record("hasClass", class)

	if m.HasClassFunc == nil {
		return false
	}

	return m.HasClassFunc(class)
}

// Construct implements platform.Runtime.
func (m *Runtime) Construct(class string, arguments ...any) (any, error) {
	m.record("construct", class, arguments)

	if m.ConstructFunc == nil {
		return nil, nil
	}

	return m.ConstructFunc(class, arguments)
}

// GetExtensions implements platform.Runtime.
func (m *Runtime) GetExtensions() []string {
	m.record("getExtensions")

	if m.GetExtensionsFunc == nil {
		return []string{}
	}

	return m.GetExtensionsFunc()
}

// GetExtensionVersion implements platform.Runtime.
func (m *Runtime) GetExtensionVersion(extension string) string {
	m.record("getExtensionVersion", extension)

	if m.GetExtensionVersionFunc == nil {
		return ""
	}

	return m.GetExtensionVersionFunc(extension)
}

// GetExtensionInfo implements platform.Runtime.
func (m *Runtime) GetExtensionInfo(extension string) (string, error) {
	m.record("getExtensionInfo", extension)

	if m.GetExtensionInfoFunc == nil {
		return "", nil
	}

	return m.GetExtensionInfoFunc(extension)
}

// ConstantMap is willReturnCallback over [name => value] as Composer's
// tests write it: keys are ltrim($class.'::'.$name, ':'). It returns a
// HasConstantFunc (isset) and a GetConstantFunc (?? null).
func ConstantMap(constants map[string]any) (has func(constant, class string) bool, get func(constant, class string) (any, error)) {
	key := func(constant, class string) string {
		k := constant
		if class != "" {
			k = class + "::" + constant
		}

		return k
	}

	has = func(constant, class string) bool {
		v, ok := constants[key(constant, class)]

		return ok && v != nil
	}

	get = func(constant, class string) (any, error) {
		return constants[key(constant, class)], nil
	}

	return has, get
}

// InvokeEntry is one row of willReturnMap for invoke: the callable, its
// arguments and the value returned.
type InvokeEntry struct {
	Callable  platform.Callable
	Arguments []any
	Return    any
}

// InvokeMap is willReturnMap for invoke: the Return of the entry whose
// callable and arguments equal the call's (as PHPUnit compares them), or
// null.
func InvokeMap(entries []InvokeEntry) func(platform.Callable, []any) (any, error) {
	return func(callable platform.Callable, arguments []any) (any, error) {
		for _, e := range entries {
			if e.Callable == callable && sameArguments(e.Arguments, arguments) {
				return e.Return, nil
			}
		}

		return nil, nil
	}
}

func sameArguments(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if !php.StrictEquals(normalize(a[i]), normalize(b[i])) {
			return false
		}
	}

	return true
}

func normalize(v any) any {
	if i, ok := v.(int); ok {
		return int64(i)
	}

	return v
}

// HhvmDetector is a platform.HhvmVersionDetector returning Version.
type HhvmDetector struct {
	Version string
}

// GetVersion implements platform.HhvmVersionDetector.
func (d HhvmDetector) GetVersion() string { return d.Version }
