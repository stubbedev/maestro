package repository

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/testutil"
)

// preparedRepo is a repository of a/a and b/b written once to dir's
// vendor/composer, with an InstallationManager for it.
func preparedRepo(t *testing.T) (repo *FilesystemRepository, a *pkg.CompletePackage, im installPaths, dir string) {
	t.Helper()
	dir = testutil.RealTempDir(t) // realpath()ed, as the repository compares it
	t.Chdir(dir)
	file := must(json.NewFile(dir+"/vendor/composer/installed.json", nil, nil))
	repo = must(NewFilesystemRepository(file, true, getRootPackage(t, "__root__", "dev-master")))
	repo.SetDevPackageNames([]string{"b/b"})
	im = installPaths(func(p pkg.PackageInterface) string { return dir + "/vendor/" + p.Name() })
	a = getPackage(t, "a/a", "1.0")
	noErr(t, repo.AddPackage(a))
	noErr(t, repo.AddPackage(getPackage(t, "b/b", "1.0")))
	noErr(t, repo.Write(true, im))

	return repo, a, im, dir
}

func readInstalled(t *testing.T, dir string) (installedJSON, installedPHP string) {
	t.Helper()

	return string(must(os.ReadFile(filepath.Join(dir, "vendor/composer/installed.json")))),
		string(must(os.ReadFile(filepath.Join(dir, "vendor/composer/installed.php"))))
}

// A prepared write of an unchanged repository writes what Write writes.
func TestFilesystemRepository_PrepareWriteUnchanged(t *testing.T) {
	repo, _, im, dir := preparedRepo(t)
	wantJSON, wantPHP := readInstalled(t, dir)
	repo.PrepareWrite(true, im, []string{"b/b"})
	if repo.preparedWrite == nil {
		t.Fatal("PrepareWrite prepared nothing")
	}
	noErr(t, repo.Write(true, im))
	if repo.preparedWrite != nil {
		t.Fatal("Write left the prepared write")
	}
	gotJSON, gotPHP := readInstalled(t, dir)
	if gotJSON != wantJSON || gotPHP != wantPHP {
		t.Fatalf("prepared write wrote\n%s\n%s\nwant\n%s\n%s", gotJSON, gotPHP, wantJSON, wantPHP)
	}
}

// A package changed in place between PrepareWrite and Write (a plugin
// setting its extra, say) is written as it is then, not as it was
// prepared: the prepared write is keyed on the packages' revisions, not
// only on which packages they are.
func TestFilesystemRepository_PrepareWriteChangedInPlace(t *testing.T) {
	repo, a, im, dir := preparedRepo(t)
	repo.PrepareWrite(true, im, []string{"b/b"})
	repo.preparedWrite.Wait() // the build reads a; change it after
	key := repo.preparedWrite.Key()
	a.SetExtra(php.ArrayOf("changed", "in place"))
	if key.same(keyOf(key.in)) {
		t.Fatal("the key of a package changed in place is the same")
	}
	noErr(t, repo.Write(true, im))
	gotJSON, gotPHP := readInstalled(t, dir)
	if !strings.Contains(gotJSON, `"changed": "in place"`) {
		t.Fatalf("installed.json misses the change:\n%s", gotJSON)
	}

	// what a write without a prepared one gives
	noErr(t, repo.Write(true, im))
	wantJSON, wantPHP := readInstalled(t, dir)
	if gotJSON != wantJSON || gotPHP != wantPHP {
		t.Fatalf("Write after a change in place wrote\n%s\n%s\nwant\n%s\n%s", gotJSON, gotPHP, wantJSON, wantPHP)
	}
}

// The root package changed in place is not the one the write was
// prepared of either.
func TestFilesystemRepository_PrepareWriteRootChanged(t *testing.T) {
	repo, _, im, _ := preparedRepo(t)
	repo.PrepareWrite(true, im, []string{"b/b"})
	repo.preparedWrite.Wait()
	key := repo.preparedWrite.Key()
	root, ok := repo.rootPackage.(*pkg.RootPackage)
	if !ok {
		t.Fatalf("root package is a %T", repo.rootPackage)
	}
	root.SetExtra(php.ArrayOf("changed", "in place"))
	if key.same(keyOf(key.in)) {
		t.Fatal("the key of a root package changed in place is the same")
	}
	repo.DiscardPreparedWrite()
}

// DiscardPreparedWrite drops the prepared write, and Write builds its own.
func TestFilesystemRepository_DiscardPreparedWrite(t *testing.T) {
	repo, _, im, dir := preparedRepo(t)
	wantJSON, wantPHP := readInstalled(t, dir)
	repo.PrepareWrite(true, im, []string{"b/b"})
	repo.DiscardPreparedWrite()
	repo.DiscardPreparedWrite()
	if repo.preparedWrite != nil {
		t.Fatal("DiscardPreparedWrite left the prepared write")
	}
	noErr(t, repo.Write(true, im))
	gotJSON, gotPHP := readInstalled(t, dir)
	if gotJSON != wantJSON || gotPHP != wantPHP {
		t.Fatalf("Write after a discard wrote\n%s\n%s\nwant\n%s\n%s", gotJSON, gotPHP, wantJSON, wantPHP)
	}
}
