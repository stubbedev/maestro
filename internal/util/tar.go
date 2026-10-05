// Ports src/Composer/Util/Tar.php, reading the archive the way PharData
// presents it.

package util

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
)

// errNoComposerJSON is thrown when neither the archive root nor its single
// top level directory holds a composer.json.
var errNoComposerJSON = errors.New("No composer.json found either at the top level or within the topmost directory")

// multipleTopLevelDirsError is thrown for archives without a root
// composer.json and more than one top level directory.
func multipleTopLevelDirsError(paths []string) error {
	return errors.New("Archive has more than one top level directories, and no composer.json was found on the top level, so it's an invalid archive. Top level paths found were: " + strings.Join(paths, ","))
}

// TarGetComposerJSON ports Tar::getComposerJson: the root composer.json of
// a tar, tar.gz or tar.bz2 archive, or the one in its single top level
// directory. ok is false when the archive does not exist or is empty.
func TarGetComposerJSON(pathToArchive string) (content string, ok bool, err error) {
	f, err := os.Open(pathToArchive)
	if err != nil {
		// PharData opens a missing file as a new, empty archive.
		return "", false, nil
	}
	defer f.Close()

	entries, err := readPharTar(f)
	if err != nil {
		return "", false, errors.New(`internal corruption of phar "` + pathToArchive + `" (truncated entry)`)
	}

	// The root of a PharData lists the first path segment of each entry,
	// sorted, without "." and "..".
	roots := map[string]bool{}

	for name, entry := range entries {
		if strings.HasPrefix(name, ".phar") {
			continue
		}

		first, _, nested := strings.Cut(name, "/")
		if first == "" || first == "." || first == ".." {
			continue
		}

		roots[first] = roots[first] || nested || entry.isDir
	}

	if len(roots) == 0 {
		return "", false, nil
	}

	if entry, ok := pharLookup(entries, "composer.json"); ok {
		content, err := pharContent(entry, "composer.json", pathToArchive)

		return content, err == nil, err
	}

	names := make([]string, 0, len(roots))
	for name := range roots {
		names = append(names, name)
	}

	slices.Sort(names)

	var topLevelPaths []string

	for _, name := range names {
		if roots[name] {
			topLevelPaths = append(topLevelPaths, name)
			if len(topLevelPaths) > 1 {
				return "", false, multipleTopLevelDirsError(topLevelPaths)
			}
		}
	}

	if len(topLevelPaths) > 0 {
		path := topLevelPaths[0] + "/composer.json"
		if entry, ok := pharLookup(entries, path); ok {
			content, err := pharContent(entry, path, pathToArchive)

			return content, err == nil, err
		}
	}

	return "", false, errNoComposerJSON
}

type pharEntry struct {
	isDir   bool
	content []byte
}

// pharLookup is isset($phar[$path]): a file or directory entry, or a
// directory implied by the entries below it.
func pharLookup(entries map[string]*pharEntry, path string) (*pharEntry, bool) {
	if entry, ok := entries[path]; ok {
		return entry, true
	}

	for name := range entries {
		if strings.HasPrefix(name, path+"/") {
			return &pharEntry{isDir: true}, true
		}
	}

	return nil, false
}

// pharContent is PharFileInfo::getContent.
func pharContent(entry *pharEntry, path, archive string) (string, error) {
	if entry.isDir {
		return "", errors.New(`phar error: Cannot retrieve contents, "` + path + `" in tar archive "` + archive + `" is a directory`)
	}

	return string(entry.content), nil
}

// readPharTar reads the entries of a possibly gzip or bzip2 compressed tar,
// keyed by name without the trailing slash of directories; a later entry
// replaces an earlier one of the same name. Only composer.json candidates
// keep their content.
func readPharTar(r io.Reader) (map[string]*pharEntry, error) {
	br := bufio.NewReader(r)

	magic, _ := br.Peek(3)

	var stream io.Reader = br

	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		gz, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		defer gz.Close()

		stream = gz
	case bytes.HasPrefix(magic, []byte("BZh")):
		stream = bzip2.NewReader(br)
	}

	entries := map[string]*pharEntry{}
	tr := tar.NewReader(stream)

	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return entries, nil
		}

		if err != nil {
			return nil, err
		}

		name := hdr.Name
		isDir := hdr.Typeflag == tar.TypeDir || strings.HasSuffix(name, "/")
		name = strings.TrimSuffix(name, "/")

		entry := &pharEntry{isDir: isDir}

		// Only a composer.json at the root or one level down can be read.
		if !isDir && phpBasename(name, false) == "composer.json" && strings.Count(name, "/") <= 1 {
			if entry.content, err = io.ReadAll(tr); err != nil {
				return nil, err
			}
		}

		entries[name] = entry
	}
}
