package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/json/res"
	"github.com/stubbedev/maestro/internal/util"
)

// copyTree copies a fixture project into dir.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// project is a fixture project installed in-process by maestro with the
// plugin runtime wired in, as cmd/maestro does.
type project struct {
	t       *testing.T
	dir     string
	rt      *Runtime
	factory *composer.Factory
	out     *io.BufferIO
	stdout  *os.File
}

func newProject(t *testing.T, fixture string, verbosity int, configure ...func(*Options)) *project {
	t.Helper()

	dir := t.TempDir()
	if real, ok := util.RealpathOK(dir); ok {
		dir = real
	}
	copyTree(t, filepath.Join("testdata", "projects", fixture), dir)
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("COMPOSER_HOME", home)
	t.Setenv("COMPOSER_CACHE_DIR", filepath.Join(home, "cache"))
	t.Setenv("COMPOSER_NO_INTERACTION", "1")
	t.Chdir(dir)

	out, err := io.NewBufferIO("", verbosity, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}

	stdout, err := os.Create(filepath.Join(t.TempDir(), "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdout.Close() })

	f := &composer.Factory{Runtime: composer.NewRuntime("test", nil)}
	opts := Options{CacheDir: filepath.Join(home, "cache"), Args: []string{"maestro", "install"}, Stdout: stdout, Stderr: stdout}
	for _, c := range configure {
		c(&opts)
	}
	rt, err := Setup(f, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(rt.Close)

	return &project{t: t, dir: dir, rt: rt, factory: f, out: out, stdout: stdout}
}

// install runs `composer install` (an update without a lock file) in the
// project.
func (p *project) install(devMode bool) (int, error) {
	p.t.Helper()

	c, err := p.factory.CreateComposer(p.out, nil, composer.PluginsEnabled, "", false)
	if err != nil {
		return 0, err
	}
	installer, err := composer.CreateInstaller(p.out, c)
	if err != nil {
		return 0, err
	}
	installer.SetDevMode(devMode).SetAudit(false)

	return installer.Run()
}

func (p *project) output() string {
	return strings.ReplaceAll(p.out.Output(), "\r", "")
}

func (p *project) childOutput() string {
	data, err := os.ReadFile(p.stdout.Name())
	if err != nil {
		p.t.Fatal(err)
	}

	return string(data)
}

func TestPlugins_API(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "api", console.VerbosityNormal)

	// No lock file: an update.
	code, err := p.install(true)
	output := p.output()
	if err != nil || code != 0 {
		t.Fatalf("update = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	for _, want := range []string{
		"activate MaestroTest\\ApiPlugin\\Plugin Composer\\IO\\BufferIO verbose=false",
		"pre-package-install maestro-test/tool-lib binaries=bin/tool",
		"post-package-install maestro-test/tool-lib ops=2 Composer\\DependencyResolver\\Operation\\InstallOperation",
		"pre-autoload-dump root psr-4 now Project\\,Generated\\",
		// makeAutoloader's loader was built before the root's autoload
		// changed (its hash only covers the packages), as in Composer.
		"autoload dumped, generated class exists: false",
		"post-install post-update-cmd",
		"vendor-dir: ./vendor",
		"packages: maestro-test/api-plugin@1.2.3 maestro-test/tool-lib@0.1.0",
		"self: maestro-test/api-plugin composer-plugin extra=42",
		"install path: vendor/maestro-test/api-plugin",
		"requires: composer-plugin-api ^2.0 Composer\\Semver\\Constraint\\MultiConstraint",
		"installed versions: 1.2.3",
		"process: captured",
		"locked: true",
		// JsonFile::COMPOSER_SCHEMA_PATH and LOCK_SCHEMA_PATH name the
		// schemas, wherever the shim is (issue #38).
		"schemas: " + sha256Hex(res.ComposerSchema()) + " " + sha256Hex(res.LockSchema()),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("update output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}

	// The root autoload change reached the generated autoloader.
	psr4, err := os.ReadFile(filepath.Join(p.dir, "vendor", "composer", "autoload_psr4.php"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(psr4), `'Generated\\' => array($baseDir . '/generated')`) {
		t.Errorf("autoload_psr4.php:\n%s", psr4)
	}

	// The binaries removed before the installation, the extra added to the
	// installed package after it.
	if _, err := os.Lstat(filepath.Join(p.dir, "vendor", "bin", "tool")); !os.IsNotExist(err) {
		t.Errorf("vendor/bin/tool: %v", err)
	}
	installed, err := os.ReadFile(filepath.Join(p.dir, "vendor", "composer", "installed.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), `"maestro-test": "marked by the plugin"`) {
		t.Errorf("installed.json:\n%s", installed)
	}

	// From the lock file, in the same process: a second Composer
	// instance loads the plugin again (its class renamed, as the class
	// exists already).
	p.out, err = io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}
	code, err = p.install(true)
	output = p.output()
	if err != nil || code != 0 {
		t.Fatalf("install = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	if final := p.rt.Finish(0); final != 0 {
		t.Errorf("Finish = %d", final)
	}

	for _, want := range []string{
		"activate MaestroTest\\ApiPlugin\\Plugin_composer_tmp0 Composer\\IO\\BufferIO verbose=false",
		"post-install post-install-cmd",
		"locked: true",
		"running command: NULL",
		"script post-install-cmd dev=true",
		"script vendor-dir=vendor",
		"script root=maestro-test/api-project 1.0.0+no-version-set",
		"custom args=a1 originating=no",
		"script dispatch custom=1",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("install output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}

	// The plugin's listener runs before the root's script (priority 10).
	if i, j := strings.Index(output, "post-install post-install-cmd"), strings.Index(output, "script post-install-cmd"); i < 0 || j < 0 || i > j {
		t.Errorf("listener order:\n%s", output)
	}

	// Platform::putEnv() in the script reached the next script, a plain
	// putenv() did not (Symfony Process passes what getenv() and $_SERVER
	// both have).
	if got := p.childOutput() + output; !strings.Contains(got, "from-php|\n") {
		t.Errorf("the @php script saw the wrong variables:\n%s\nchild:\n%s", output, p.childOutput())
	}
}

// TestPlugins_Installers runs custom installers (docs/PLUGINS.md §5.6)
// through maestro's InstallationManager: a LibraryInstaller subclass
// whose getInstallPath()/installCode() overrides the Go base honours and
// which chains parent::install()'s promise, a direct InstallerInterface
// implementation, a MetapackageInstaller subclass, a legacy
// composer-installer, addInstaller/removeInstaller/getInstaller identity,
// and a rejected promise reaching maestro as the PHP exception.
func TestPlugins_Installers(t *testing.T) {
	requirePHP(t)

	p := newProject(t, "installers", console.VerbosityNormal)

	code, err := p.install(true)
	output := p.output()
	if err != nil || code != 0 {
		t.Fatalf("update = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	for _, want := range []string{
		"getInstaller is the object: true",
		"after removeInstaller: Composer\\Installer\\LibraryInstaller",
		"plugin installer: Composer\\Installer\\PluginInstaller same=true",
		"PearInstaller exists: false",
		"classmap: MaestroTest\\Installers\\CustomInstaller,MaestroTest\\Installers\\FailedException,MaestroTest\\Installers\\FailingInstaller,MaestroTest\\Installers\\IfaceInstaller,MaestroTest\\Installers\\PackInstaller,MaestroTest\\Installers\\Plugin",
		"binary caller: php",
		// LibraryInstaller::getInstallPath() appends to the realpath() of
		// the vendor dir with a slash.
		"custom install path: " + filepath.Join(p.dir, "vendor") + "/maestro-test/installers-plugin",
		"Loading \"maestro-test/legacy-installer\" which is a legacy composer-installer built for Composer 1.x, it is likely to cause issues as you are running Composer 2.x.",
		"installCode local/custom-a at custom/custom-a",
		"parent::install() gave a promise: true",
		"installed local/custom-a in repo=true file=true vendorDir resolved=true",
		"pack installed local/pack-a path=NULL",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("update output lacks %q:\n%s\nchild:\n%s", want, output, p.childOutput())
		}
	}
	for _, file := range []string{"custom/custom-a/file.php", "iface/iface-a", "legacy-installed/legacy-a/file.php"} {
		if _, err := os.Stat(filepath.Join(p.dir, file)); err != nil {
			t.Errorf("%s: %v", file, err)
		}
	}
	installedPHP, err := os.ReadFile(filepath.Join(p.dir, "vendor", "composer", "installed.php"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`'install_path' => __DIR__ . '/../../custom/custom-a',`,
		`'install_path' => __DIR__ . '/../../iface/iface-a',`,
		`'install_path' => __DIR__ . '/../../legacy-installed/legacy-a',`,
		`'install_path' => null,`,
	} {
		if !strings.Contains(string(installedPHP), want) {
			t.Errorf("installed.php lacks %q:\n%s", want, installedPHP)
		}
	}

	// A rejected promise of the installer fails the operation with the
	// PHP exception.
	data, err := os.ReadFile(filepath.Join(p.dir, "composer.json"))
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), `"local/legacy-a": "*"`, `"local/legacy-a": "*", "local/failing-a": "*"`, 1))
	if err := os.WriteFile(filepath.Join(p.dir, "composer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.dir, "composer.lock")); err != nil {
		t.Fatal(err)
	}
	p.out, err = io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.install(true)
	output = p.output()
	var e *PHPException
	if !errors.As(err, &e) || e.Class != `MaestroTest\Installers\FailedException` || e.Message != "could not install local/failing-a" {
		t.Fatalf("update with a failing installer: %v (%T)\n%s\nchild:\n%s", err, err, output, p.childOutput())
	}
	for _, want := range []string{"failing local/failing-a", "    Install of local/failing-a failed"} {
		if !strings.Contains(output, want) {
			t.Errorf("output lacks %q:\n%s", want, output)
		}
	}

	// The custom installer's uninstall() chains the promise of
	// parent::uninstall(), pending while the removal runs: its callback
	// runs once the files are gone.
	data = []byte(strings.Replace(strings.Replace(string(data), `, "local/failing-a": "*"`, "", 1), `"local/custom-a": "*",`, "", 1))
	if err := os.WriteFile(filepath.Join(p.dir, "composer.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(p.dir, "composer.lock")); err != nil {
		t.Fatal(err)
	}
	p.out, err = io.NewBufferIO("", console.VerbosityNormal, console.NewOutputFormatter(false))
	if err != nil {
		t.Fatal(err)
	}
	code, err = p.install(true)
	output = p.output()
	if err != nil || code != 0 {
		t.Fatalf("update removing local/custom-a = %d, %v\n%s\nchild:\n%s", code, err, output, p.childOutput())
	}
	if i, j := strings.Index(output, "  - Removing local/custom-a (1.0.0)"), strings.Index(output, "uninstalled local/custom-a file=false"); i < 0 || j < i {
		t.Errorf("removal output:\n%s\nchild:\n%s", output, p.childOutput())
	}
	if final := p.rt.Finish(0); final != 0 {
		t.Errorf("Finish = %d", final)
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}
