// Ordered string-keyed maps standing in for the PHP arrays of
// src/Composer/Policy keyed by package name, advisory ID, severity or list
// name.

package policy

import "iter"

// Entry is one key and value of an OrderedMap.
type Entry[V any] struct {
	Key   string
	Value V
}

// OrderedMap is a PHP array with string keys: insertion ordered, setting
// an existing key replaces its value in place. The zero value is empty.
// The maps are small, so lookups are linear.
type OrderedMap[V any] struct {
	entries []Entry[V]
}

// Len returns the number of entries.
func (m *OrderedMap[V]) Len() int {
	if m == nil {
		return 0
	}

	return len(m.entries)
}

func (m *OrderedMap[V]) find(key string) int {
	if m == nil {
		return -1
	}
	for i := range m.entries {
		if m.entries[i].Key == key {
			return i
		}
	}

	return -1
}

// Get returns the value at key.
func (m *OrderedMap[V]) Get(key string) (V, bool) {
	if i := m.find(key); i >= 0 {
		return m.entries[i].Value, true
	}
	var zero V

	return zero, false
}

// Has reports whether key is set.
func (m *OrderedMap[V]) Has(key string) bool { return m.find(key) >= 0 }

// Set sets key to value, keeping the position of an existing key.
func (m *OrderedMap[V]) Set(key string, value V) {
	if i := m.find(key); i >= 0 {
		m.entries[i].Value = value

		return
	}
	m.entries = append(m.entries, Entry[V]{Key: key, Value: value})
}

// Keys returns the keys in order (array_keys).
func (m *OrderedMap[V]) Keys() []string {
	keys := make([]string, m.Len())
	for i := range keys {
		keys[i] = m.entries[i].Key
	}

	return keys
}

// Entries returns the entries in order. Do not modify them.
func (m *OrderedMap[V]) Entries() []Entry[V] {
	if m == nil {
		return nil
	}

	return m.entries
}

// All iterates over the entries in order.
func (m *OrderedMap[V]) All() iter.Seq2[string, V] {
	return func(yield func(string, V) bool) {
		for _, e := range m.Entries() {
			if !yield(e.Key, e.Value) {
				return
			}
		}
	}
}

// Clone returns a copy of m (the values themselves are shared).
func (m *OrderedMap[V]) Clone() *OrderedMap[V] {
	c := &OrderedMap[V]{}
	if m != nil {
		c.entries = append(make([]Entry[V], 0, len(m.entries)), m.entries...)
	}

	return c
}

// Reasons maps names to ignore reasons; a nil reason is PHP's null.
type Reasons = OrderedMap[*string]
