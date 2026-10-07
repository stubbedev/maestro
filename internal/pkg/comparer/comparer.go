// Ports src/Composer/Package/Comparer/Comparer.php.

package comparer

import (
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
			hash := source.files[dir].hashes[f]
			if dh, ok := destination.lookup(dir, f); ok {
				if hash != dh {
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

// tree is doTree's array: directory => file => hash, in readdir order.
type tree struct {
	dirs  []string
	files map[string]*dirFiles
}

type dirFiles struct {
	keys   []string
	hashes map[string]string
}

func (t *tree) set(dir, file, hash string) {
	d, ok := t.files[dir]
	if !ok {
		d = &dirFiles{hashes: map[string]string{}}
		t.files[dir] = d
		t.dirs = append(t.dirs, dir)
	}

	if _, ok := d.hashes[file]; !ok {
		d.keys = append(d.keys, file)
	}

	d.hashes[file] = hash
}

func (t *tree) lookup(dir, file string) (string, bool) {
	d, ok := t.files[dir]
	if !ok {
		return "", false
	}

	h, ok := d.hashes[file]

	return h, ok
}

// doTree ports Comparer::doTree for the tree rooted at base (PHP's "."
// after chdir). PHP's placeholder entry for "only directories so far" never
// holds files, so it is not modelled.
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
			target, _ := os.Readlink(full)
			t.set(dir, name, target)
		case php.IsDir(full):
			if !walk(base, rel, t) {
				return false
			}
		case info.Mode().IsRegular() && info.Size() > 0:
			if hash, err := php.Sha1File(full); err == nil {
				t.set(dir, name, hash)
			}
		}
	}

	return true
}
