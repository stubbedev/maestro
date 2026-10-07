// Ports src/Composer/Package/Comparer/Comparer.php.

package comparer

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Comparer ports Composer\Package\Comparer\Comparer: it lists the files
// that differ between two directory trees.
//
// Composer hashes files with xxh3; only equality matters, so sha1 is used
// here.
type Comparer struct {
	source  string
	update  string
	changed *section
}

// section is the ordered map $changed: section key => list of paths.
type section struct {
	keys  []string
	items map[string][]string
}

func (s *section) add(key, item string) {
	if _, ok := s.items[key]; !ok {
		s.keys = append(s.keys, key)
	}

	s.items[key] = append(s.items[key], item)
}

// SetSource ports Comparer::setSource.
func (c *Comparer) SetSource(source string) { c.source = source }

// SetUpdate ports Comparer::setUpdate.
func (c *Comparer) SetUpdate(update string) { c.update = update }

// Changed is one section of Comparer::getChanged: "changed", "removed" or
// "added" and its paths.
type Changed struct {
	Section string
	Paths   []string
}

// GetChanged ports Comparer::getChanged; nil is PHP's false (nothing
// changed). With explicated, each path is followed by " (section)".
func (c *Comparer) GetChanged(explicated bool) []Changed {
	if c.changed == nil || len(c.changed.keys) == 0 {
		return nil
	}

	out := make([]Changed, 0, len(c.changed.keys))

	for _, key := range c.changed.keys {
		paths := append([]string(nil), c.changed.items[key]...)
		if explicated {
			for i, p := range paths {
				paths[i] = p + " (" + key + ")"
			}
		}

		out = append(out, Changed{Section: key, Paths: paths})
	}

	return out
}

// GetChangedAsString ports Comparer::getChangedAsString (toString is
// unused, as in PHP).
func (c *Comparer) GetChangedAsString(toString, explicated bool) string {
	_ = toString

	changed := c.GetChanged(explicated)
	if changed == nil {
		return ""
	}

	var strs []string

	for _, s := range changed {
		for _, p := range s.Paths {
			strs = append(strs, p+"\r\n")
		}
	}

	return strings.Trim(strings.Join(strs, "\r\n"), php.TrimChars)
}

// DoCompare ports Comparer::doCompare. PHP changes into each directory and
// walks "."; paths are reported relative to it, as "./dir/file". PHP
// exits the process when the update tree cannot be read; that is an error
// here.
//
// PHP hashes every file of both trees. Only the files of both are
// compared, so only theirs are read here, and only when it takes reading
// them: the same file (a hard link) holds the same content, and files of
// different sizes hold different contents unless both cannot be read.
func (c *Comparer) DoCompare() error {
	c.changed = &section{items: map[string][]string{}}

	source, ok := doTree(c.source)
	if !ok {
		return nil
	}

	destination, ok := doTree(c.update)
	if !ok {
		return &os.PathError{Op: "opendir", Path: c.update, Err: os.ErrNotExist}
	}

	for _, dir := range source.dirs {
		for _, f := range source.files[dir].keys {
			if d, ok := destination.lookup(dir, f); ok {
				if !source.files[dir].entries[f].same(d) {
					c.changed.add("changed", dir+"/"+f)
				}
			} else {
				c.changed.add("removed", dir+"/"+f)
			}
		}
	}

	for _, dir := range destination.dirs {
		for _, f := range destination.files[dir].keys {
			if _, ok := source.lookup(dir, f); !ok {
				c.changed.add("added", dir+"/"+f)
			}
		}
	}

	return nil
}

// entry is a file doTree records: a symlink, valued by its target, or a
// non-empty regular file, valued by its hash, computed when asked. As in
// PHP, a value that cannot be had (readlink or hash_file failing) is
// false, which only equals false.
type entry struct {
	path string
	info fs.FileInfo

	known bool
	value string
	ok    bool
}

// get returns the entry's value; ok false is PHP's false.
func (e *entry) get() (value string, ok bool) {
	if !e.known {
		e.known = true
		if e.info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(e.path)
			e.value, e.ok = target, err == nil
		} else {
			hash, err := php.Sha1File(e.path)
			e.value, e.ok = hash, err == nil
		}
	}

	return e.value, e.ok
}

// readable reports whether the regular file's content can be had.
func (e *entry) readable() bool {
	if e.known {
		return e.ok
	}
	f, err := os.Open(e.path)
	if err != nil {
		return false
	}
	_ = f.Close()

	return true
}

// same reports whether e and d have the same value ($hash === $other).
func (e *entry) same(d *entry) bool {
	if e.info.Mode().IsRegular() && d.info.Mode().IsRegular() {
		if os.SameFile(e.info, d.info) {
			return true
		}
		if e.info.Size() != d.info.Size() {
			return !e.readable() && !d.readable()
		}
	}
	ev, eok := e.get()
	dv, dok := d.get()

	return eok == dok && ev == dv
}

// tree is doTree's array: directory => file => entry, in readdir order.
type tree struct {
	dirs  []string
	files map[string]*dirFiles
}

type dirFiles struct {
	keys    []string
	entries map[string]*entry
}

func (t *tree) set(dir, file string, e *entry) {
	d, ok := t.files[dir]
	if !ok {
		d = &dirFiles{entries: map[string]*entry{}}
		t.files[dir] = d
		t.dirs = append(t.dirs, dir)
	}

	if _, ok := d.entries[file]; !ok {
		d.keys = append(d.keys, file)
	}

	d.entries[file] = e
}

func (t *tree) lookup(dir, file string) (*entry, bool) {
	d, ok := t.files[dir]
	if !ok {
		return nil, false
	}

	e, ok := d.entries[file]

	return e, ok
}

// doTree ports Comparer::doTree for the tree rooted at base (PHP's "."
// after chdir), leaving the values to be had when compared. PHP's
// placeholder entry for "only directories so far" never holds files, so
// it is not modelled.
func doTree(base string) (*tree, bool) {
	t := &tree{files: map[string]*dirFiles{}}

	return t, walk(base, ".", t)
}

func walk(base, dir string, t *tree) bool {
	f, err := os.Open(filepath.Join(base, dir))
	if err != nil {
		return false
	}

	names, err := f.Readdirnames(-1)
	_ = f.Close()

	if err != nil {
		return false
	}

	for _, name := range names {
		// while ($file = readdir($dh)) stops at an entry named "0".
		if name == "0" {
			break
		}

		rel := dir + "/" + name
		full := filepath.Join(base, rel)

		info, err := os.Lstat(full)
		if err != nil {
			continue
		}

		switch {
		case info.Mode()&os.ModeSymlink != 0:
			t.set(dir, name, &entry{path: full, info: info})
		case php.IsDir(full):
			if !walk(base, rel, t) {
				return false
			}
		case info.Mode().IsRegular() && info.Size() > 0:
			t.set(dir, name, &entry{path: full, info: info})
		}
	}

	return true
}
