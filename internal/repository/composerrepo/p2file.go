// Ports nothing: a decoded metadata file whose version lists are decoded
// only when read (deliberate deviation 3, speed).

package composerrepo

import (
	"github.com/stubbedev/maestro/internal/php"
)

// p2File is a metadata file, as decodeArray decodes its JSON. A file read
// back from the decoded cache (a p2Slot) decodes a package's version list
// only when it is read, and may carry an index of the versions
// (p2Index). A p2File is handed to one reader at a time, as the array it
// stands for would be: what the reader changes in what it reads is its
// own.
type p2File struct {
	// data is the file, decoded at once; nil for a slot
	data *php.Array
	slot *p2Slot
}

// eagerFile is the p2File of data, decoded at once; nil for nil.
func eagerFile(data *php.Array) *p2File {
	if data == nil {
		return nil
	}

	return &p2File{data: data}
}

// at is $data[$key], for a top-level key other than "packages".
func (f *p2File) at(key string) any {
	switch {
	case f == nil:
		return nil
	case f.slot != nil:
		return f.slot.top.At(key)
	default:
		return f.data.At(key)
	}
}

// versions is $data['packages'][$name] ?? null: the version list of name.
func (f *p2File) versions(name string) any {
	switch {
	case f == nil:
		return nil
	case f.slot != nil:
		return f.slot.versions(name)
	default:
		return packageVersions(f.data, name)
	}
}

// hasVersions is isset($data['packages'][$name]).
func (f *p2File) hasVersions(name string) bool {
	switch {
	case f == nil:
		return false
	case f.slot != nil:
		return f.slot.hasVersions(name)
	default:
		return packageVersions(f.data, name) != nil
	}
}

// array is the whole file.
func (f *p2File) array() *php.Array {
	switch {
	case f == nil:
		return nil
	case f.slot != nil:
		return f.slot.array()
	default:
		return f.data
	}
}

// index is the index of name's versions; nil when there is none.
func (f *p2File) index(name string) *p2Index {
	if f == nil || f.slot == nil {
		return nil
	}

	return f.slot.index(name)
}

// slim is slimFile of the file.
func (f *p2File) slim() *php.Array {
	if f.slot != nil {
		return f.slot.slim()
	}

	return slimFile(f.data)
}

// packageVersions is $data['packages'][$name] ?? null.
func packageVersions(data *php.Array, name string) any {
	packages, _ := data.At("packages").(*php.Array)

	return packages.At(name)
}
