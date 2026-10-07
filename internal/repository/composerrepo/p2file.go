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
	// spans are where the values of data's version lists are in its JSON
	// (php.JSONDecodeSpans), which a slot keeps instead of the values; nil
	// when not known
	spans map[*php.Array][]php.JSONSpan
	// drafts are, by package name, what the readers of data found out
	// about the version lists that their slot's index keeps
	drafts map[string]*indexDraft
	slot   *p2Slot
}

// indexDraft is what the readers of a file decoded at once found out
// about a minified version list that its slot's index keeps, which the
// slot is built from instead of finding it out again: the versions
// scanned (scanVersions; nil when not scanned), and the index built
// while a reader expanded the whole list (nil when none did).
type indexDraft struct {
	scanned []scannedVersion
	built   *indexBuilder
}

// draft is the indexDraft of name's version list; nil for a file not
// decoded at once.
func (f *p2File) draft(name string) *indexDraft {
	if f == nil || f.data == nil {
		return nil
	}
	d := f.drafts[name]
	if d == nil {
		if f.drafts == nil {
			f.drafts = map[string]*indexDraft{}
		}
		d = &indexDraft{}
		f.drafts[name] = d
	}

	return d
}

// scannedFor is the versions of items scanned; nil when not scanned.
func (d *indexDraft) scannedFor(items []any) []scannedVersion {
	if d == nil || len(d.scanned) != len(items) {
		return nil
	}

	return d.scanned
}

// builder is a new indexBuilder of items for a reader expanding them
// all, which the slot takes; nil for a nil draft.
func (d *indexDraft) builder(items []any) *indexBuilder {
	if d == nil {
		return nil
	}
	d.built = newIndexBuilder(items, d.scannedFor(items))

	return d.built
}

// builtFor is the index built of items, every version added; nil when
// there is none.
func (d *indexDraft) builtFor(items []any) *indexBuilder {
	if d == nil || d.built == nil || d.built.added != len(items) || len(d.built.items) != len(items) {
		return nil
	}

	return d.built
}

// eagerFile is the p2File of data, decoded at once; nil for nil.
func eagerFile(data *php.Array) *p2File {
	if data == nil {
		return nil
	}

	return &p2File{data: data}
}

// decodeFile is eagerFile(decodeArray(json)), with the spans of its
// version lists' entries.
func decodeFile(json string) *p2File {
	v, spans, err := php.JSONDecodeSpans(json, php.JSONDefaultDepth, 2)
	if data, ok := v.(*php.Array); ok && err == nil {
		return &p2File{data: data, spans: spans}
	}

	return nil
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
