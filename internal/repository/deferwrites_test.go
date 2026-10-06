package repository

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/pkg"
)

// Deferred writes leave the files the last Write asked for would have
// written: a package added after it (an operation that failed before its
// write) is not in them.
func TestFilesystemRepository_DeferWrites(t *testing.T) {
	run := func(deferred bool) map[string]string {
		dir := t.TempDir()
		t.Chdir(dir)
		file := must(json.NewFile(dir+"/vendor/composer/installed.json", nil, nil))
		repo := must(NewFilesystemRepository(file, true, getRootPackage(t, "__root__", "dev-master")))
		repo.SetDevPackageNames([]string{"b/b"})
		im := installPaths(func(p pkg.PackageInterface) string { return dir + "/vendor/" + p.Name() })
		if deferred {
			repo.DeferWrites()
		}
		for _, name := range []string{"c/c", "a/a", "b/b"} {
			noErr(t, repo.AddPackage(getPackage(t, name, "1.0")))
			noErr(t, repo.Write(true, im))
		}
		noErr(t, repo.AddPackage(getPackage(t, "d/d", "1.0")))
		if deferred {
			if _, err := os.Stat(dir + "/vendor/composer/installed.json"); err == nil {
				t.Fatal("a deferred write wrote")
			}
			noErr(t, repo.FlushWrites())
		}
		files := map[string]string{}
		for _, name := range []string{"installed.json", "installed.php", "InstalledVersions.php"} {
			files[name] = string(must(os.ReadFile(filepath.Join(dir, "vendor/composer", name))))
		}

		return files
	}

	want, got := run(false), run(true)
	for name, content := range want {
		if got[name] != content {
			t.Errorf("%s differs:\n%s\nwant:\n%s", name, got[name], content)
		}
	}
}
