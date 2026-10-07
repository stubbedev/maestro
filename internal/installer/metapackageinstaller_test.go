package installer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

func TestMetapackageInstaller_Install(t *testing.T) {
	io := newBufferIO(t)
	installer := NewMetapackageInstaller(io)
	p := newPackage("foo/meta", "1.0.0")
	repo := newMockRepo(t)

	if _, err := installer.Install(repo, p); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, repo.rec.list(), []string{"addPackage foo/meta-1.0.0.0"})

	if got := php.NormalizeEOL(io.Output()); got != "  - Installing foo/meta (1.0.0)\n" {
		t.Errorf("output %q", got)
	}
}

func TestMetapackageInstaller_Update(t *testing.T) {
	io := newBufferIO(t)
	installer := NewMetapackageInstaller(io)
	initial := newPackage("foo/meta", "1.0.0")
	target := newPackage("foo/meta", "1.0.1")
	repo := newMockRepo(t, true, false)

	if _, err := installer.Update(repo, initial, target); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, repo.rec.list(), []string{"hasPackage foo/meta-1.0.0.0", "removePackage foo/meta-1.0.0.0", "addPackage foo/meta-1.0.1.0"})

	if got := php.NormalizeEOL(io.Output()); got != "  - Upgrading foo/meta (1.0.0 => 1.0.1)\n" {
		t.Errorf("output %q", got)
	}

	_, err := installer.Update(repo, initial, target)

	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Errorf("err = %v, want InvalidArgumentException", err)
	}
}

func TestMetapackageInstaller_Uninstall(t *testing.T) {
	io := newBufferIO(t)
	installer := NewMetapackageInstaller(io)
	p := newPackage("foo/meta", "1.0.0")
	repo := newMockRepo(t, true, false)

	if _, err := installer.Uninstall(repo, p); err != nil {
		t.Fatal(err)
	}

	equalCalls(t, repo.rec.list(), []string{"hasPackage foo/meta-1.0.0.0", "removePackage foo/meta-1.0.0.0"})

	if got := php.NormalizeEOL(io.Output()); got != "  - Removing foo/meta (1.0.0)\n" {
		t.Errorf("output %q", got)
	}

	_, err := installer.Uninstall(repo, p)

	if !phperr.InstanceOf(err, "InvalidArgumentException") {
		t.Errorf("err = %v, want InvalidArgumentException", err)
	}
}

func TestNoopInstaller(t *testing.T) {
	installer := NewNoopInstaller()
	p := newPackage("foo/bar", "1.0.0")
	target := newPackage("foo/bar", "2.0.0")

	repo := newMockRepo(t)
	repo.fake = false

	if _, err := installer.Install(repo, p); err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Update(repo, p, target); err != nil {
		t.Fatal(err)
	}

	if _, err := installer.Uninstall(repo, target); err != nil {
		t.Fatal(err)
	}

	var iae *util.InvalidArgumentError
	if _, err := installer.Uninstall(repo, target); !errors.As(err, &iae) || iae.Message != "Package is not installed: foo/bar-2.0.0.0" {
		t.Errorf("err = %v", err)
	}

	p.SetTargetDir(pkg.Str("Some/Dir"))

	if path, ok, _ := installer.InstallPath(p); !ok || path != "foo/bar/Some/Dir" {
		t.Errorf("InstallPath = %q", path)
	}
}

func TestProjectInstaller(t *testing.T) {
	dir := t.TempDir()
	dm := newMockDM()
	// ProjectInstaller turns every backslash of its path into a slash.
	installer := NewProjectInstaller(dir+`\project\`, dm, util.NewFilesystem(nil))

	p := newPackage("foo/bar", "1.0.0")

	if path, _, _ := installer.InstallPath(p); path != filepath.ToSlash(dir)+"/project/" {
		t.Errorf("InstallPath = %q", path)
	}

	if _, err := installer.Download(p, nil); err != nil {
		t.Fatal(err)
	}

	if !isDir(dir + "/project") {
		t.Error("project dir not created")
	}

	if err := os.WriteFile(dir+"/project/file", nil, 0o644); err != nil {
		t.Fatal(err)
	}

	var iae *util.InvalidArgumentError
	if _, err := installer.Download(p, nil); !errors.As(err, &iae) || iae.Message != "Project directory "+filepath.ToSlash(dir)+"/project/ is not empty." {
		t.Errorf("err = %v", err)
	}

	if _, err := installer.Update(nil, p, p); !errors.As(err, &iae) || iae.Message != "not supported" {
		t.Errorf("err = %v", err)
	}

	equalCalls(t, dm.rec.list(), []string{"download foo/bar-1.0.0.0 " + filepath.ToSlash(dir) + "/project/"})
}
