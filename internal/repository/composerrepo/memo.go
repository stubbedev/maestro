// Ports nothing: the metadata files a run already decoded, kept so that
// reading them again does not decode them again (deliberate deviation 3,
// speed).

package composerrepo

import (
	"sync"

	"github.com/stubbedev/maestro/internal/php"
)

// decodedFiles remembers metadata files decoded before they are read:
//
//   - per cache key, the parts of the last file decoded for it that the
//     security advisory and filter list loads read (slimFile). An update
//     reads each p2 file three times, for the packages, their advisories
//     and their filter entries; only the first decodes it all.
//   - files a speculation decoded ahead of the package loads
//     (speculate.go), each handed over once; a load waits for a cached
//     file the speculation is about to hand over rather than decode it a
//     second time.
//
// A remembered file is only used for the exact JSON it was decoded from,
// so it is what decoding that JSON gives.
type decodedFiles struct {
	mu   sync.Mutex
	slim map[string]slimEntry
	// ahead are the files decoded by the speculation of generation gen;
	// requested the URLs the package loads requested.
	ahead     map[string]decodedFile
	gen       int
	requested map[string]struct{}
	// prebuilt are the packages built ahead from the files taken.
	prebuilt map[*p2File]*prebuilt
	// expected are the cached files the speculation of generation gen
	// decodes and hands over without waiting for anything else
	expected map[string]*expectedFile
}

// expectedFile is a file the speculation is to offer, decoded from json;
// done is closed once it did, or gave up.
type expectedFile struct {
	json string
	done chan struct{}
}

type decodedFile struct {
	json string
	file *p2File
	pre  *prebuilt
}

// slimEntry is the slim form of the file last decoded from json.
type slimEntry struct {
	json string
	data *php.Array
}

// rememberSlim keeps the slim form of file, decoded from json, for the
// file cached under cacheKey.
func (d *decodedFiles) rememberSlim(cacheKey, json string, file *p2File) {
	if json == "" || file == nil {
		return
	}
	slim := file.slim()

	d.mu.Lock()
	defer d.mu.Unlock()

	if d.slim == nil {
		d.slim = map[string]slimEntry{}
	}
	d.slim[cacheKey] = slimEntry{json: json, data: slim}
}

// slimOf is (a copy of) the slim form of the file cached under cacheKey
// when json is the JSON it was decoded from, else nil.
func (d *decodedFiles) slimOf(cacheKey, json string) *p2File {
	d.mu.Lock()
	defer d.mu.Unlock()

	if e, ok := d.slim[cacheKey]; ok && e.json == json {
		return eagerFile(e.data.Clone())
	}

	return nil
}

// offer hands file, decoded from json by the speculation of generation
// gen, and the packages it built ahead from it (pre, may be nil) to the
// next package load of the file cached under cacheKey; the speculation
// must not use them any more.
func (d *decodedFiles) offer(gen int, cacheKey, json string, file *p2File, pre *prebuilt) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if gen != d.gen {
		return
	}
	if d.ahead == nil {
		d.ahead = map[string]decodedFile{}
	}
	d.ahead[cacheKey] = decodedFile{json: json, file: file, pre: pre}
}

// expect notes that the speculation of generation gen is to offer the
// file cached under cacheKey, decoded from json; the returned function
// (to be called whatever happens, more than once if need be) tells that it
// did or gave up.
func (d *decodedFiles) expect(gen int, cacheKey, json string) (done func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if gen != d.gen {
		return func() {}
	}
	if d.expected == nil {
		d.expected = map[string]*expectedFile{}
	}
	e := &expectedFile{json: json, done: make(chan struct{})}
	d.expected[cacheKey] = e

	var once sync.Once

	return func() {
		once.Do(func() {
			d.mu.Lock()
			if d.expected[cacheKey] == e {
				delete(d.expected, cacheKey)
			}
			d.mu.Unlock()
			close(e.done)
		})
	}
}

// take returns, once, the file offered for the file cached under
// cacheKey when json is what it was decoded from, else nil; when the
// speculation is to offer it (expect), it waits for that. The packages
// built ahead from it are then prebuiltFor(file).
func (d *decodedFiles) take(cacheKey, json string) *p2File {
	if d == nil {
		return nil
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	e, ok := d.ahead[cacheKey]
	if expected := d.expected[cacheKey]; !ok && expected != nil && expected.json == json {
		d.mu.Unlock()
		<-expected.done
		d.mu.Lock()
		e, ok = d.ahead[cacheKey]
	}
	if !ok || e.json != json {
		return nil
	}
	delete(d.ahead, cacheKey)
	if e.pre != nil {
		if d.prebuilt == nil {
			d.prebuilt = map[*p2File]*prebuilt{}
		}
		d.prebuilt[e.file] = e.pre
	}

	return e.file
}

// prebuiltFor returns, once, the packages built ahead from file, a file
// take returned (nil for none).
func (d *decodedFiles) prebuiltFor(file *p2File) *prebuilt {
	d.mu.Lock()
	defer d.mu.Unlock()

	pre := d.prebuilt[file]
	delete(d.prebuilt, file)

	return pre
}

// startSpeculation returns the generation of a new speculation.
func (d *decodedFiles) startSpeculation() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.gen++

	return d.gen
}

// stopSpeculation drops what the speculation of generation gen offered
// and nothing took.
func (d *decodedFiles) stopSpeculation(gen int) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if gen == d.gen {
		d.gen++
		d.ahead = nil
		d.prebuilt = nil
		d.expected = nil
	}
}

// markRequested records that the package loads requested url.
func (d *decodedFiles) markRequested(url string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.requested == nil {
		d.requested = map[string]struct{}{}
	}
	d.requested[url] = struct{}{}
}

// wasRequested tells whether the package loads requested url.
func (d *decodedFiles) wasRequested(url string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	_, ok := d.requested[url]

	return ok
}

// slimFile is a metadata file reduced to what the advisory and filter
// loads read of it: every top-level key but "packages" as it is (a deep
// copy, as the package loading may still change data), and "packages"
// with each package's versions replaced by true, or kept null, since only
// isset($response['packages'][$name]) is asked of it.
func slimFile(data *php.Array) *php.Array {
	return slimFileOf(data, func(php.Key) bool { return false })
}

// slimFileOf is slimFile of data, a file whose version lists are null
// where slotted tells that they are set (a p2Slot's top).
func slimFileOf(data *php.Array, slotted func(name php.Key) bool) *php.Array {
	slim := php.NewArrayCap(data.Len())
	for k, v := range data.All() {
		switch a := v.(type) {
		case *php.Array:
			if k == php.StrKey("packages") {
				names := php.NewArrayCap(a.Len())
				for name, versions := range a.All() {
					if versions == nil && !slotted(name) {
						names.SetKey(name, nil)
					} else {
						names.SetKey(name, true)
					}
				}
				v = names
			} else {
				v = a.Clone()
			}
		case *php.Object:
			v = a.Clone()
		}
		slim.SetKey(k, v)
	}

	return slim
}
