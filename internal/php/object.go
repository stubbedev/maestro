// Ports the parts of stdClass (Zend/zend_object_handlers.c) that Composer
// relies on: ordered dynamic properties, (array) and (object) casts.

package php

import "iter"

// Object is a stdClass instance: ordered properties with string names
// ("0" stays the string "0"). It encodes as a JSON object even when empty.
// Like PHP objects, *Object has reference semantics.
type Object struct {
	props Array
}

// NewObject returns an empty stdClass.
func NewObject() *Object {
	o := &Object{}
	o.props.next = noNextFree
	return o
}

// Len returns the number of properties.
func (o *Object) Len() int { return o.props.live }

// Get returns $o->$name.
func (o *Object) Get(name string) (any, bool) { return o.props.GetKey(rawStrKey(name)) }

// Has reports property_exists($o, $name).
func (o *Object) Has(name string) bool { return o.props.find(rawStrKey(name)) >= 0 }

// Set performs $o->$name = $v.
func (o *Object) Set(name string, v any) { o.props.SetKey(rawStrKey(name), v) }

// Delete performs unset($o->$name).
func (o *Object) Delete(name string) { o.props.DeleteKey(rawStrKey(name)) }

// All iterates over the properties in order, with foreach semantics.
func (o *Object) All() iter.Seq2[string, any] {
	return func(yield func(string, any) bool) {
		for k, v := range o.props.All() {
			if !yield(k.s, v) {
				return
			}
		}
	}
}

// Keys returns the property names in order.
func (o *Object) Keys() []string {
	ks := make([]string, 0, o.props.live)
	for k := range o.props.All() {
		ks = append(ks, k.s)
	}
	return ks
}

// Clone returns a deep copy.
func (o *Object) Clone() *Object {
	c := NewObject()
	for k, v := range o.props.All() {
		c.props.insert(k, cloneValue(v))
	}
	return c
}

// ToArray performs (array) $o: numeric property names become int keys.
// Values are shared, not cloned.
func (o *Object) ToArray() *Array {
	a := NewArrayCap(o.props.live)
	for k, v := range o.props.All() {
		a.SetKey(StrKey(k.s), v)
	}
	return a
}

// ObjectFromArray performs (object) $a: int keys become string property
// names. Values are shared, not cloned.
func ObjectFromArray(a *Array) *Object {
	o := NewObject()
	for k, v := range a.All() {
		o.props.insert(rawStrKey(k.String()), v)
	}
	return o
}
