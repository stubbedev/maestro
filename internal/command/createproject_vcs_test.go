package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// create-project removes the VCS directories in the order Symfony's Finder
// lists them: readdir() order, which the live php oracle reads with
// FilesystemIterator (the iterator Finder's RecursiveDirectoryIterator
// extends).
func TestVcsDirectories_ReaddirOrder(t *testing.T) {
	phpBin, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php is not available")
	}

	dir := t.TempDir()

	for _, name := range []string{".hg", "src", "_darcs", ".git", "CVS", ".bzr", "_FOSSIL_", ".svn", "vendor", ".monotone", "_svn", ".fslckout", ".arch-params"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o777); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(filepath.Join(dir, ".gitx"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.CommandContext(t.Context(), phpBin, "-r", `
		$names = ['.svn', '_svn', 'CVS', '_darcs', '.arch-params', '.monotone', '.bzr', '.git', '.hg', '.fslckout', '_FOSSIL_'];
		foreach (new FilesystemIterator($argv[1]) as $file) {
			if ($file->isDir() && in_array($file->getFilename(), $names, true)) {
				echo $file->getPathname(), "\n";
			}
		}`, dir).Output()
	if err != nil {
		t.Fatal(err)
	}

	got, err := vcsDirectories(dir)
	if err != nil {
		t.Fatal(err)
	}

	if want := strings.Fields(string(out)); !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}
