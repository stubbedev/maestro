// Handles (docs/PLUGINS.md §5.3): every object crossing the channel has
// exactly one owner and one handle, positive for Go-owned objects
// (allocated here), negative for PHP-owned ones (allocated by the shim).
// Handles are never reused and nothing is released, so identity is
// trivial: a handle always stands for the same object on both sides.

package rpc

import (
	"errors"
	"reflect"
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
)

// Handle identifies an object crossing the channel. A *php.Array may hold
// one: it encodes as the object it stands for.
type Handle int64

// PHPOpaque implements php.Opaque.
func (Handle) PHPOpaque() {}

// Object is a Go-owned object that can cross to PHP, as a service proxy
// or, when it also implements Mirror, a data mirror. Implementations must
// be pointers (handles are by identity). As php.Opaque values, objects go
// into *php.Array values (snapshots, event arguments) as they are.
type Object interface {
	php.Opaque
	// PHPClass is the PHP class of the object's proxy or mirror.
	php.Classer
}

// Mirror is a Go-owned data object PHP keeps a copy of
// (docs/PLUGINS.md §5.3): PHP's copy is refreshed whenever Rev changes,
// and the fields PHP's setters change come back through ApplyMirror.
type Mirror interface {
	Object
	// MirrorBase is the shim base class of the object's family, whose
	// MirrorAdapter builds the PHP object.
	MirrorBase() string
	// Rev is a counter every setter increments (docs/PLUGINS.md §7).
	Rev() uint64
	// MirrorSnapshot returns every field, by PHP property name. Values
	// are encoded like any other value (objects become handles).
	MirrorSnapshot() (*php.Array, error)
	// ApplyMirror applies the fields PHP changed, through the normal
	// setters.
	ApplyMirror(fields *php.Array) error
}

// DeltaMirror is a Mirror that can send only what changed since a
// revision; otherwise the full snapshot is sent.
type DeltaMirror interface {
	Mirror
	// MirrorChanges returns the fields changed since revision since; ok
	// is false when they are not known (the full snapshot goes instead).
	MirrorChanges(since uint64) (fields *php.Array, ok bool)
}

// MirrorFactory builds the Go object of a PHP-born mirror (new Package()
// in plugin code) the first time it crosses to Go. class is its PHP class,
// which may be a user subclass of the factory's base.
type MirrorFactory func(class string, snapshot *php.Array) (Mirror, error)

// PHPObject is a PHP-owned object (a plugin, a closure, a PHP installer,
// ...) as Go holds it. The same handle always gives the same pointer.
type PHPObject struct {
	H Handle
	// Class is get_class(); Base the nearest shim base class of a mirror
	// ("" otherwise).
	Class, Base string
	// Snapshot is a PHP mirror's snapshot when it first crossed (no
	// factory took it); nil otherwise.
	Snapshot any
}

// PHPOpaque implements php.Opaque.
func (*PHPObject) PHPOpaque() {}

// handles is the Go side's handle table.
type handles struct {
	last Handle // the last Go handle allocated

	goObjects map[Handle]Object
	goIDs     map[Object]Handle
	// sent holds the Go handles PHP has the class (and snapshot) of.
	sent map[Handle]bool

	phpObjects map[Handle]*PHPObject
	// adopted maps the PHP handle of a PHP-born object Go registered (a
	// mirror, or a service maestro created for it) to its Go object.
	adopted map[Handle]Object

	// mirrors are the Go mirrors PHP holds, with the revision it has, in
	// the order PHP got them.
	mirrors   []*mirrorState
	mirrorIdx map[Handle]*mirrorState
}

type mirrorState struct {
	h   Handle
	m   Mirror
	rev uint64
}

func newHandles() handles {
	return handles{
		goObjects:  map[Handle]Object{},
		goIDs:      map[Object]Handle{},
		sent:       map[Handle]bool{},
		phpObjects: map[Handle]*PHPObject{},
		adopted:    map[Handle]Object{},
		mirrorIdx:  map[Handle]*mirrorState{},
	}
}

// handleOf returns the handle of a Go object, allocating one.
func (t *handles) handleOf(o Object) (Handle, error) {
	if h, ok := t.goIDs[o]; ok {
		return h, nil
	}
	if !reflect.TypeOf(o).Comparable() || reflect.TypeOf(o).Kind() != reflect.Pointer {
		return 0, errors.New("rpc: a Go object crossing to PHP must be a pointer, not " + reflect.TypeOf(o).String())
	}

	t.last++
	t.goObjects[t.last] = o
	t.goIDs[o] = t.last

	return t.last, nil
}

// Lookup returns the Go object or *PHPObject of a known handle.
func (t *handles) lookup(h Handle) (any, bool) {
	if h > 0 {
		o, ok := t.goObjects[h]

		return o, ok
	}
	if m, ok := t.adopted[h]; ok {
		return m, true
	}
	o, ok := t.phpObjects[h]

	return o, ok
}

func unknownHandle(h Handle) error {
	return &ProtocolError{Message: "unknown handle " + strconv.FormatInt(int64(h), 10)}
}
