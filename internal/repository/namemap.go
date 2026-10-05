// Ordered maps standing in for the PHP arrays keyed by package name that
// the repository API passes around.

package repository

import (
	"iter"

	"github.com/stubbedev/maestro/internal/semver"
)

// NameMap is a PHP array keyed by package (or list) name: insertion
// ordered, setting an existing key replaces its value in place, unset
// keeps the order of the others. The zero value is empty and ready to use;
// a nil *NameMap reads as empty.
type NameMap[V any] struct {
	entries []nameEntry[V]
	index   map[string]int
	deleted int
}

type nameEntry[V any] struct {
	key   string
	value V
	live  bool
}

// ConstraintMap is array<string, ?ConstraintInterface>: package names to
// constraints, a nil constraint standing for PHP's null.
type ConstraintMap = NameMap[semver.ConstraintInterface]

// NewConstraintMap returns the map [name => constraint] for one name.
func NewConstraintMap(name string, constraint semver.ConstraintInterface) *ConstraintMap {
	m := &ConstraintMap{}
	m.Set(name, constraint)

	return m
}

// Len returns count($m).
func (m *NameMap[V]) Len() int {
	if m == nil {
		return 0
	}

	return len(m.entries) - m.deleted
}

// Get returns $m[$key].
func (m *NameMap[V]) Get(key string) (V, bool) {
	if m != nil {
		if i, ok := m.index[key]; ok {
			return m.entries[i].value, true
		}
	}
	var zero V

	return zero, false
}

// Has reports array_key_exists($key, $m).
func (m *NameMap[V]) Has(key string) bool {
	if m == nil {
		return false
	}
	_, ok := m.index[key]

	return ok
}

// Set performs $m[$key] = $value.
func (m *NameMap[V]) Set(key string, value V) {
	if i, ok := m.index[key]; ok {
		m.entries[i].value = value

		return
	}
	if m.index == nil {
		m.index = make(map[string]int)
	}
	m.index[key] = len(m.entries)
	m.entries = append(m.entries, nameEntry[V]{key: key, value: value, live: true})
}

// Delete performs unset($m[$key]).
func (m *NameMap[V]) Delete(key string) {
	i, ok := m.index[key]
	if !ok {
		return
	}
	delete(m.index, key)
	m.entries[i] = nameEntry[V]{}
	m.deleted++
	if m.deleted > 16 && m.deleted*2 > len(m.entries) {
		m.compact()
	}
}

func (m *NameMap[V]) compact() {
	live := m.entries[:0]
	for _, e := range m.entries {
		if e.live {
			m.index[e.key] = len(live)
			live = append(live, e)
		}
	}
	clear(m.entries[len(live):])
	m.entries = live
	m.deleted = 0
}

// All iterates over the keys and values in order. The map must not be
// modified during the loop.
func (m *NameMap[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		if m == nil {
			return
		}
		for _, e := range m.entries {
			if e.live && !yield(e.key, e.value) {
				return
			}
		}
	}
}

// Keys returns array_keys($m).
func (m *NameMap[V]) Keys() []string {
	keys := make([]string, 0, m.Len())
	for k := range m.All() {
		keys = append(keys, k)
	}

	return keys
}

// Clone returns a copy, as assigning a PHP array copies it; the values
// are shared.
func (m *NameMap[V]) Clone() *NameMap[V] {
	c := &NameMap[V]{}
	if m == nil {
		return c
	}
	c.entries = make([]nameEntry[V], 0, m.Len())
	c.index = make(map[string]int, m.Len())
	for k, v := range m.All() {
		c.index[k] = len(c.entries)
		c.entries = append(c.entries, nameEntry[V]{key: k, value: v, live: true})
	}

	return c
}
