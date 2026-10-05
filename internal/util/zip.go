// Ports src/Composer/Util/Zip.php.

package util

import (
	"archive/zip"
	"io"
	"slices"
	"strings"

	"github.com/stubbedev/maestro/internal/phperr"
)

// ZipGetComposerJSON ports Zip::getComposerJson: the root composer.json of
// a zip archive, or the one in its single top level directory. ok is false
// where PHP returns null: the archive cannot be opened or is empty.
func ZipGetComposerJSON(pathToZip string) (content string, ok bool, err error) {
	zr, err := zip.OpenReader(pathToZip)
	if err != nil {
		return "", false, nil
	}
	defer func() { _ = zr.Close() }()

	if len(zr.File) == 0 {
		return "", false, nil
	}

	data, err := zipLocateFile(zr.File, "composer.json")
	if err != nil {
		return "", false, err
	}

	return string(data), true, nil
}

// zipLocateFile ports Zip::locateFile, returning the content of the root
// file, or of the one in the single top level directory. ZipArchive's
// locateName returns the first entry of a name, and getFromIndex fails for
// an entry it cannot read.
func zipLocateFile(files []*zip.File, filename string) ([]byte, error) {
	// Return the root file if it is there and is a file.
	if data, ok := zipReadName(files, filename); ok {
		return data, nil
	}

	var topLevelPaths []string

	addTopLevel := func(site phperr.Site, path string) error {
		if slices.Contains(topLevelPaths, path) {
			return nil
		}

		topLevelPaths = append(topLevelPaths, path)
		if len(topLevelPaths) > 1 {
			return multipleTopLevelDirsError(site, topLevelPaths)
		}

		return nil
	}

	windows := IsWindows()

	for _, f := range files {
		name := f.Name

		// Ignore the OSX specific resource fork folder.
		if strings.Contains(name, "__MACOSX") {
			continue
		}

		dirname := phpDirname(name, windows)

		// Handle archives with a proper TOC.
		if dirname == "." {
			if err := addTopLevel(phperr.At("Zip.php", 81), name); err != nil {
				return nil, err
			}

			continue
		}

		// Handle archives which do not have a TOC record for the directory
		// itself.
		if !strings.ContainsAny(dirname, `\/`) {
			if err := addTopLevel(phperr.At("Zip.php", 90), dirname+"/"); err != nil {
				return nil, err
			}
		}
	}

	if len(topLevelPaths) > 0 {
		if data, ok := zipReadName(files, topLevelPaths[0]+filename); ok {
			return data, nil
		}
	}

	return nil, noComposerJSONError(phperr.At("Zip.php", 99))
}

// zipReadName is ZipArchive::locateName without flags (the first entry of
// exactly that name), then getFromIndex.
func zipReadName(files []*zip.File, name string) ([]byte, bool) {
	i := slices.IndexFunc(files, func(f *zip.File) bool { return f.Name == name })
	if i < 0 {
		return nil, false
	}

	rc, err := files[i].Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)

	return data, err == nil
}
