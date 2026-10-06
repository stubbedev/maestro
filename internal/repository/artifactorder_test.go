package repository

import (
	"archive/zip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// ArtifactRepository finds the archives in the order PHP's
// RecursiveIteratorIterator(RecursiveDirectoryIterator(FOLLOW_SYMLINKS))
// yields them: readdir() order, descending into each directory where it
// is met. The live php oracle walks the same tree.
func TestArtifactRepository_ReaddirOrder(t *testing.T) {
	phpBin, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php is not available")
	}

	dir := t.TempDir()

	names := []string{"m.zip", "b.zip", "sub/z.zip", "a.zip", "sub/deeper/q.zip", "y.zip", "c.zip", "x/k.zip", "d.zip", "sub/a.zip", "p.zip"}
	for i, name := range names {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o777); err != nil {
			t.Fatal(err)
		}

		writeArtifactZip(t, path, `{"name": "test/p`+string(rune('a'+i))+`", "version": "1.0.0"}`)
	}

	out, err := exec.CommandContext(t.Context(), phpBin, "-r", `
		$it = new RegexIterator(new RecursiveIteratorIterator(new RecursiveDirectoryIterator($argv[1], RecursiveDirectoryIterator::FOLLOW_SYMLINKS)), '/^.+\.(zip|tar|gz|tgz)$/i');
		foreach ($it as $file) {
			echo substr($file->getPathname(), strlen($argv[1]) + 1), "\n";
		}`, dir).Output()
	if err != nil {
		t.Fatal(err)
	}

	var want []string

	for path := range strings.FieldsSeq(string(out)) {
		want = append(want, "test/p"+string(rune('a'+slices.Index(names, filepath.ToSlash(path)))))
	}

	repo := must(NewArtifactRepository(php.ArrayOf("type", "artifact", "url", dir), io.NewNullIO()))

	var got []string
	for _, p := range must(repo.Packages()) {
		got = append(got, p.PrettyName())
	}

	if !slices.Equal(got, want) {
		t.Errorf("got  %v\nwant %v (php: %q)", got, want, out)
	}
}

func writeArtifactZip(t *testing.T, path, composerJSON string) {
	t.Helper()

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	w := zip.NewWriter(f)

	entry, err := w.Create("composer.json")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write([]byte(composerJSON)); err != nil {
		t.Fatal(err)
	}

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
