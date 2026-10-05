// Ports src/Composer/Util/Zip.php.

package util

import (
	"archive/zip"
	"io"
	"strings"
)

// ZipGetComposerJSON ports Zip::getComposerJson: the root composer.json of
// a zip archive, or the one in its single top level directory. ok is false
// when the archive cannot be opened, is empty or the file cannot be read.
func ZipGetComposerJSON(pathToZip string) (content string, ok bool, err error) {
	zr, err := zip.OpenReader(pathToZip)
	if err != nil {
		return "", false, nil
	}
	defer zr.Close()

	if len(zr.File) == 0 {
		return "", false, nil
	}

	file, err := zipLocateFile(zr.File, "composer.json")
	if err != nil {
		return "", false, err
	}

	data, ok := zipRead(file)

	return string(data), ok, nil
}

// zipLocateFile finds the root file name, or the one in the single top
// level directory.
func zipLocateFile(files []*zip.File, filename string) (*zip.File, error) {
	// Return the root file if it is there and is a file.
	if f := zipLocateName(files, filename); f != nil {
		if _, ok := zipRead(f); ok {
			return f, nil
		}
	}

	var topLevelPaths []string

	addTopLevel := func(path string) error {
		for _, p := range topLevelPaths {
			if p == path {
				return nil
			}
		}

		topLevelPaths = append(topLevelPaths, path)
		if len(topLevelPaths) > 1 {
			return multipleTopLevelDirsError(topLevelPaths)
		}

		return nil
	}

	for _, f := range files {
		name := f.Name
		dirname := phpDirname(name, IsWindows())

		// Ignore the OSX specific resource fork folder.
		if strings.Contains(name, "__MACOSX") {
			continue
		}

		// Handle archives with a proper TOC.
		if dirname == "." {
			if err := addTopLevel(name); err != nil {
				return nil, err
			}

			continue
		}

		// Handle archives which do not have a TOC record for the directory
		// itself.
		if !strings.ContainsAny(dirname, `\/`) {
			if err := addTopLevel(dirname + "/"); err != nil {
				return nil, err
			}
		}
	}

	if len(topLevelPaths) > 0 {
		if f := zipLocateName(files, topLevelPaths[0]+filename); f != nil {
			if _, ok := zipRead(f); ok {
				return f, nil
			}
		}
	}

	return nil, errNoComposerJSON
}

// zipLocateName is ZipArchive::locateName without flags: an exact name.
func zipLocateName(files []*zip.File, name string) *zip.File {
	for _, f := range files {
		if f.Name == name {
			return f
		}
	}

	return nil
}

// zipRead is ZipArchive::getFromIndex.
func zipRead(f *zip.File) ([]byte, bool) {
	rc, err := f.Open()
	if err != nil {
		return nil, false
	}
	defer rc.Close()

	data, err := io.ReadAll(rc)

	return data, err == nil
}
