package util

import (
	"errors"
	"testing"
)

// Ports tests/Composer/Test/Util/TarTest.php and ZipTest.php. The tests for
// PHP's zip extension being absent and for the PHP < 8.0 phar guard do not
// apply. The Tar messages are those real PHP gives for the fixtures.

const composerJSONFixture = "{\n    \"name\": \"foo/bar\"\n}\n"

func assertComposerJSON(t *testing.T, name, content string, ok bool, err error) {
	t.Helper()

	if err != nil || !ok || content != composerJSONFixture {
		t.Errorf("%s = %q, %v, %v", name, content, ok, err)
	}
}

func assertNoComposerJSON(t *testing.T, name string, ok bool, err error) {
	t.Helper()

	if ok || err != nil {
		t.Errorf("%s = %v, %v; want null", name, ok, err)
	}
}

func assertArchiveError(t *testing.T, name string, err error, message string) {
	t.Helper()

	var runtimeErr *RuntimeError
	if !errors.As(err, &runtimeErr) || (message != "" && err.Error() != message) {
		t.Errorf("%s error = %v, want RuntimeException %q", name, err, message)
	}
}

const tarFixtures = "testdata/Fixtures/Tar/"

func TestTar_ReturnsNullifTheTarIsNotFound(t *testing.T) {
	_, ok, err := TarGetComposerJSON(tarFixtures + "invalid.zip")
	assertNoComposerJSON(t, "invalid.zip", ok, err)
}

func TestTar_ReturnsNullIfTheTarIsEmpty(t *testing.T) {
	_, ok, err := TarGetComposerJSON(tarFixtures + "empty.tar.gz")
	assertNoComposerJSON(t, "empty.tar.gz", ok, err)
}

func TestTar_ThrowsExceptionIfTheTarHasNoComposerJson(t *testing.T) {
	_, _, err := TarGetComposerJSON(tarFixtures + "nojson.tar.gz")
	assertArchiveError(t, "nojson.tar.gz", err, errNoComposerJSON.Message)
}

func TestTar_ThrowsExceptionIfTheComposerJsonIsInASubSubfolder(t *testing.T) {
	_, _, err := TarGetComposerJSON(tarFixtures + "subfolders.tar.gz")
	assertArchiveError(t, "subfolders.tar.gz", err, errNoComposerJSON.Message)
}

func TestTar_ReturnsComposerJsonInTarRoot(t *testing.T) {
	content, ok, err := TarGetComposerJSON(tarFixtures + "root.tar.gz")
	assertComposerJSON(t, "root.tar.gz", content, ok, err)
}

func TestTar_ReturnsComposerJsonInFirstFolder(t *testing.T) {
	content, ok, err := TarGetComposerJSON(tarFixtures + "folder.tar.gz")
	assertComposerJSON(t, "folder.tar.gz", content, ok, err)
}

func TestTar_MultipleTopLevelDirsIsInvalid(t *testing.T) {
	_, _, err := TarGetComposerJSON(tarFixtures + "multiple.tar.gz")
	assertArchiveError(t, "multiple.tar.gz", err, errNoComposerJSON.Message)
}

const zipFixtures = "testdata/Fixtures/Zip/"

func TestZip_ReturnsNullifTheZipIsNotFound(t *testing.T) {
	_, ok, err := ZipGetComposerJSON(zipFixtures + "invalid.zip")
	assertNoComposerJSON(t, "invalid.zip", ok, err)
}

func TestZip_ReturnsNullIfTheZipIsEmpty(t *testing.T) {
	_, ok, err := ZipGetComposerJSON(zipFixtures + "empty.zip")
	assertNoComposerJSON(t, "empty.zip", ok, err)
}

func TestZip_ThrowsExceptionIfTheZipHasNoComposerJson(t *testing.T) {
	_, _, err := ZipGetComposerJSON(zipFixtures + "nojson.zip")
	assertArchiveError(t, "nojson.zip", err, "No composer.json found either at the top level or within the topmost directory")
}

func TestZip_ThrowsExceptionIfTheComposerJsonIsInASubSubfolder(t *testing.T) {
	_, _, err := ZipGetComposerJSON(zipFixtures + "subfolders.zip")
	assertArchiveError(t, "subfolders.zip", err, "No composer.json found either at the top level or within the topmost directory")
}

func TestZip_ReturnsComposerJsonInZipRoot(t *testing.T) {
	content, ok, err := ZipGetComposerJSON(zipFixtures + "root.zip")
	assertComposerJSON(t, "root.zip", content, ok, err)
}

func TestZip_ReturnsComposerJsonInFirstFolder(t *testing.T) {
	content, ok, err := ZipGetComposerJSON(zipFixtures + "folder.zip")
	assertComposerJSON(t, "folder.zip", content, ok, err)
}

func TestZip_MultipleTopLevelDirsIsInvalid(t *testing.T) {
	_, _, err := ZipGetComposerJSON(zipFixtures + "multiple.zip")
	assertArchiveError(t, "multiple.zip", err, "Archive has more than one top level directories, and no composer.json was found on the top level, so it's an invalid archive. Top level paths found were: folder1/,folder2/")
}

func TestZip_ReturnsComposerJsonFromFirstSubfolder(t *testing.T) {
	content, ok, err := ZipGetComposerJSON(zipFixtures + "single-sub.zip")
	assertComposerJSON(t, "single-sub.zip", content, ok, err)
}
