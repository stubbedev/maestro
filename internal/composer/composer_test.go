package composer

// Ports tests/Composer/Test/ComposerTest.php.

import (
	"regexp"
	"testing"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/locker"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/upstream"
)

func TestComposer_SetGetPackage(t *testing.T) {
	c := &Composer{}
	p := pkg.NewRootPackage("a/a", "1.0.0.0", "1.0.0")
	c.SetPackage(p)
	if c.Package() != pkg.RootPackageInterface(p) {
		t.Error("package not kept")
	}
}

func TestComposer_SetGetLocker(t *testing.T) {
	c := &Composer{}
	l, err := locker.New(newBufferIO(t), &lockFileMock{}, &installationManagerMock{}, "{}", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.SetLocker(l)
	if c.Locker() != l {
		t.Error("locker not kept")
	}
}

func TestComposer_SetGetRepositoryManager(t *testing.T) {
	c := &Composer{}
	m := repository.NewRepositoryManager(newBufferIO(t), config.New(false, ""), nil, nil, nil)
	c.SetRepositoryManager(m)
	if c.RepositoryManager() != m {
		t.Error("repository manager not kept")
	}
}

func TestComposer_SetGetDownloadManager(t *testing.T) {
	c := &Composer{}
	m := downloader.NewDownloadManager(newBufferIO(t), false, nil)
	c.SetDownloadManager(m)
	if c.DownloadManager() != m {
		t.Error("download manager not kept")
	}
}

func TestComposer_SetGetInstallationManager(t *testing.T) {
	c := &Composer{}
	m := &installationManagerMock{}
	c.SetInstallationManager(m)
	if c.InstallationManager() != InstallationManager(m) {
		t.Error("installation manager not kept")
	}
}

// TestComposer_GetVersion: a release build's version is the release maestro
// ports (internal/upstream), with the phar's form of the release date.
func TestComposer_GetVersion(t *testing.T) {
	if GetVersion() != upstream.ComposerVersion || RuntimeAPIVersion != upstream.RuntimeAPIVersion {
		t.Errorf("version = %s, runtime API %s", GetVersion(), RuntimeAPIVersion)
	}
	if !regexp.MustCompile(`^\d{4}-\d\d-\d\d \d\d:\d\d:\d\d$`).MatchString(ReleaseDate) {
		t.Errorf("release date = %q", ReleaseDate)
	}
}

func TestRuntime_RunningCommandAndOperation(t *testing.T) {
	rt := testRuntime(t, 0)
	if _, ok := rt.RunningCommand(); ok {
		t.Error("running command set")
	}
	rt.SetRunningCommand("install", true)
	rt.SetRunningOperation("update", true)
	if c, ok := rt.RunningCommand(); !ok || c != "install" {
		t.Errorf("command = %q", c)
	}
	if o, ok := rt.RunningOperation(); !ok || o != "update" {
		t.Errorf("operation = %q", o)
	}
	// a new outer command resets the operation; "" is null
	rt.SetRunningCommand("", true)
	if _, ok := rt.RunningCommand(); ok {
		t.Error(`"" is not null`)
	}
	if _, ok := rt.RunningOperation(); ok {
		t.Error("operation not reset")
	}
	if rt.PHPVersion() == "" {
		t.Error("no php version")
	}
}

func TestRuntime_Frames(t *testing.T) {
	rt := testRuntime(t, 0)
	rt.PushFrame(`Composer\Console\Application->doRun`, "app", "input")
	rt.PushFrame(`Composer\Command\InstallCommand->execute`, "command")
	if frames := rt.Frames(); len(frames) != 2 || frames[0].Object != "app" || frames[0].Function != `Composer\Console\Application->doRun` || frames[0].Args[0] != "input" || frames[1].Object != "command" {
		t.Errorf("frames = %v", frames)
	}
	rt.PopFrame()
	rt.PopFrame()
	rt.PopFrame()
	if len(rt.Frames()) != 0 {
		t.Error("frames left")
	}
}
