// The Go side of the Composer API in PHP (docs/PLUGINS.md §5.3): the
// objects maestro hands to PHP, one per underlying Go object so that
// identity holds (`$package === $rootPackage`), and the helpers the
// service handlers (svc_*.go) read their parameters with.

package plugin

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/plugin/rpc"
)

// bridge holds the Go-owned objects that crossed (or may cross) to PHP,
// keyed by the Go object they stand for.
type bridge struct {
	mu      sync.Mutex
	objects map[any]rpc.Object
}

func newBridge() *bridge { return &bridge{objects: map[any]rpc.Object{}} }

// object returns the object standing for key, creating it with create the
// first time.
func (b *bridge) object(key any, create func() rpc.Object) rpc.Object {
	b.mu.Lock()
	defer b.mu.Unlock()

	if o, ok := b.objects[key]; ok {
		return o
	}
	o := create()
	b.objects[key] = o

	return o
}

// service is a Go-owned service object (a manager, the Config, a remote
// repository, ...): PHP holds an instance of class, built without its
// constructor, whose methods call maestro.
type service struct {
	v     any
	class string
}

// PHPOpaque implements php.Opaque.
func (*service) PHPOpaque() {}

// PHPClass implements rpc.Object.
func (s *service) PHPClass() string { return s.class }

// hashable reports whether v can key the bridge's map: pointers and
// other comparable values.
func hashable(v any) bool {
	t := reflect.TypeOf(v)

	return t != nil && t.Comparable()
}

// args are the params of a call from PHP: the list the shim sends,
// receiver first for instance methods.
type args struct {
	method string
	list   []any
}

func argsOf(method string, v any) args {
	a := args{method: method}
	if arr, ok := v.(*php.Array); ok {
		a.list = arr.Values()
	}

	return a
}

// at returns param i (nil when absent: PHP's default null).
func (a args) at(i int) any {
	if i < len(a.list) {
		return a.list[i]
	}

	return nil
}

// has reports whether param i was sent and is not null.
func (a args) has(i int) bool { return a.at(i) != nil }

func (a args) str(i int) string { return php.ToString(a.at(i)) }

func (a args) integer(i int) int { return int(php.ToInt(a.at(i))) }

func (a args) boolean(i int) bool { return php.ToBool(a.at(i)) }

// array returns param i as an array; nil for null.
func (a args) array(i int) *php.Array {
	v, _ := a.at(i).(*php.Array)

	return v
}

// arrayOrEmpty returns param i as an array, [] for null.
func (a args) arrayOrEmpty(i int) *php.Array {
	if v := a.array(i); v != nil {
		return v
	}

	return php.NewArray()
}

// nullableString returns param i, ok false for null.
func (a args) nullableString(i int) (string, bool) {
	v := a.at(i)
	if v == nil {
		return "", false
	}

	return php.ToString(v), true
}

// errorf is an error of a call PHP made with params maestro cannot take
// (a bug in the shim or a TypeError PHP would have raised).
func (a args) errorf(format string, v ...any) error {
	return &typeError{msg: a.method + ": " + fmt.Sprintf(format, v...)}
}

// typeError is a \TypeError thrown back into PHP.
type typeError struct{ msg string }

func (e *typeError) Error() string { return e.msg }

// ThrowableClass implements console.Throwable.
func (*typeError) ThrowableClass() string { return "TypeError" }

// ThrowableFile implements console.Throwable.
func (*typeError) ThrowableFile() string { return "" }

// ThrowableLine implements console.Throwable.
func (*typeError) ThrowableLine() int { return 0 }

// ThrowableCode implements console.Throwable.
func (*typeError) ThrowableCode() int { return 0 }

// ThrowablePrevious implements console.Throwable.
func (*typeError) ThrowablePrevious() error { return nil }

// receiver returns param 0, the object a method is called on, as a T.
func receiver[T any](a args) (T, error) {
	return param[T](a, 0)
}

// param returns param i as a T: the service's Go object, or the param
// itself.
func param[T any](a args, i int) (T, error) {
	var zero T
	v := a.at(i)
	if s, ok := v.(*service); ok {
		v = s.v
	}
	t, ok := v.(T)
	if !ok {
		return zero, a.errorf("param %d is a %T, not a %T", i, a.at(i), zero)
	}

	return t, nil
}
