//go:build unix

package installer

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// binaryFixture is BinaryInstallerTest::setUp.
func binaryFixture(t *testing.T) (rootDir, vendorDir, binDir string) {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	mustMkdir(t, root+"/vendor")
	mustMkdir(t, root+"/bin")

	return root, root + "/vendor", root + "/bin"
}

func binPackage(bins ...any) *pkg.Package {
	p := pkg.NewPackage("deadbeef00", "1.0.0.0", "1.0.0")
	p.SetBinaries(php.ListOf(bins...))

	return p
}

var executableBinaryProvider = map[string]string{
	"simple php file":       "<?php\n\necho 'success '.$_SERVER['argv'][1];",
	"php file with shebang": "#!/usr/bin/env php\n<?php\n\necho 'success '.$_SERVER['argv'][1];",
	"phar file": func() string {
		b, _ := base64.StdEncoding.DecodeString("IyEvdXNyL2Jpbi9lbnYgcGhwCjw/cGhwCgpQaGFyOjptYXBQaGFyKCd0ZXN0LnBoYXInKTsKCnJlcXVpcmUgJ3BoYXI6Ly90ZXN0LnBoYXIvcnVuLnBocCc7CgpfX0hBTFRfQ09NUElMRVIoKTsgPz4NCj4AAAABAAAAEQAAAAEACQAAAHRlc3QucGhhcgAAAAAHAAAAcnVuLnBocCoAAADb9n9hKgAAAMUDDWGkAQAAAAAAADw/cGhwIGVjaG8gInN1Y2Nlc3MgIi4kX1NFUlZFUlsiYXJndiJdWzFdO1SOC0IE3+UN0yzrHIwyspp9slhmAgAAAEdCTUI=")

		return string(b)
	}(),
	"shebang with strict types declare": "#!/usr/bin/env php\n<?php declare(strict_types=1);\n\necho 'success '.$_SERVER['argv'][1];",
	"php file with crlf shebang":        "#!/usr/bin/env php\r\n<?php\n\necho 'success '.$_SERVER['argv'][1];",
	"shell script":                      "#!/bin/sh\nprintf 'success %s' \"$1\"\n",
}

func TestBinaryInstaller_InstallAndExecBinaryWithFullCompat(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php is not available")
	}

	for name, contents := range executableBinaryProvider {
		t.Run(name, func(t *testing.T) {
			_, vendorDir, binDir := binaryFixture(t)

			p := binPackage("binary")

			mustMkdir(t, vendorDir+"/foo/bar")

			if err := os.WriteFile(vendorDir+"/foo/bar/binary", []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}

			installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})
			if err := installer.InstallBinaries(p, vendorDir+"/foo/bar", true); err != nil {
				t.Fatal(err)
			}

			// "full" writes both proxies; on Windows cmd.exe runs the .bat.
			for _, proxy := range []string{"/binary", "/binary.bat"} {
				if _, err := os.Stat(binDir + proxy); err != nil {
					t.Errorf("proxy %s: %v", proxy, err)
				}
			}

			var output string

			proc := util.NewProcessExecutor(nil)

			if _, err := proc.Execute(util.ShellCmd(binDir+"/binary arg"), &output, ""); err != nil {
				t.Fatal(err)
			}

			if e := proc.GetErrorOutput(); e != "" {
				t.Errorf("error output %q", e)
			}

			if output != "success arg" {
				t.Errorf("output %q", output)
			}
		})
	}
}

func TestBinaryInstaller_InstallBinaryRejectsSymlinkEscapingPackageDir(t *testing.T) {
	rootDir, vendorDir, binDir := binaryFixture(t)

	p := binPackage("bin/pwn")

	// A file outside the package install directory that must not be
	// touched.
	victim := rootDir + "/victim.sh"
	if err := os.WriteFile(victim, []byte("#!/bin/sh\necho pwned\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(victim, 0o644); err != nil {
		t.Fatal(err)
	}

	before, _ := os.Stat(victim)

	installPath := vendorDir + "/attacker/pkg"
	mustMkdir(t, installPath+"/bin")

	// bin/pwn is a symlink escaping the package to the victim file
	// (GHSA-96h3-5x6v-m776).
	if err := os.Symlink("../../../../victim.sh", installPath+"/bin/pwn"); err != nil {
		t.Skip("Symbolic links are not supported on this platform")
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})
	if err := installer.InstallBinaries(p, installPath, true); err != nil {
		t.Fatal(err)
	}

	if php.FileExists(binDir + "/pwn") {
		t.Error("No vendor/bin proxy must be created for an escaping symlink bin")
	}

	if after, _ := os.Stat(victim); after.Mode() != before.Mode() {
		t.Error("A bin symlink escaping the package dir must not be chmod'd")
	}
}

func TestBinaryInstaller_InstallBinaryRejectsTraversingBinPath(t *testing.T) {
	rootDir, vendorDir, binDir := binaryFixture(t)

	// ".." bin metadata can reach BinaryInstaller without passing through
	// the solver-time ValidatingArrayLoader::validatePackage() check, e.g.
	// via the ensureBinariesPresence() re-generation loop which reads
	// packages straight from installed.json.
	p := binPackage("../../../victim.sh")

	victim := rootDir + "/victim.sh"
	if err := os.WriteFile(victim, []byte("#!/bin/sh\necho pwned\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	before, _ := os.Stat(victim)

	installPath := vendorDir + "/attacker/pkg"
	mustMkdir(t, installPath)

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})
	if err := installer.InstallBinaries(p, installPath, true); err != nil {
		t.Fatal(err)
	}

	if php.FileExists(binDir + "/victim.sh") {
		t.Error("No vendor/bin proxy must be created for a traversing bin")
	}

	if after, _ := os.Stat(victim); after.Mode() != before.Mode() {
		t.Error("A bin escaping the package dir via \"..\" must not be chmod'd")
	}
}

// TestBinaryInstaller_ChmodUnsharesHardlinks checks that making a bin
// executable does not change the file it shares an inode with (a store
// object, which hard-linked package files share).
func TestBinaryInstaller_ChmodUnsharesHardlinks(t *testing.T) {
	rootDir, vendorDir, binDir := binaryFixture(t)

	object := rootDir + "/object"
	if err := os.WriteFile(object, []byte("#!/bin/sh\necho hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustMkdir(t, vendorDir+"/foo/bar")

	if err := os.Link(object, vendorDir+"/foo/bar/tool"); err != nil {
		t.Skip("hardlinks are not supported")
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.Str(vendorDir))
	if err := installer.InstallBinaries(binPackage("tool"), vendorDir+"/foo/bar", true); err != nil {
		t.Fatal(err)
	}

	if st, _ := os.Stat(object); st.Mode().Perm() != 0o644 {
		t.Errorf("store object mode changed to %v", st.Mode())
	}

	if st, _ := os.Stat(vendorDir + "/foo/bar/tool"); st.Mode().Perm() != 0o755 {
		t.Errorf("bin mode %v", st.Mode())
	}

	if data, _ := os.ReadFile(vendorDir + "/foo/bar/tool"); string(data) != "#!/bin/sh\necho hi\n" {
		t.Errorf("bin content %q", data)
	}
}

func TestBinaryInstaller_DetermineBinaryCaller(t *testing.T) {
	rootDir, _, _ := binaryFixture(t)
	bin := rootDir + "/caller-bin"

	for _, tc := range []struct{ name, contents, want string }{
		{"env php", "#!/usr/bin/env php\n<?php", "php"},
		{"sh", "#!/bin/sh\n", "sh"},
		{"sh with argument", "#!/bin/sh -e\n", "sh -e"},
		{"php with options", "#!/usr/bin/env php -d memory_limit=-1\n", "php -d memory_limit=-1"},
		{"versioned interpreter", "#!/usr/bin/php7.4\n", "php7.4"},
		{"env with split string", "#!/usr/bin/env -S php -d x=1\n", "php -d x=1"},
		{"env with split string and no space", "#!/usr/bin/env -Sbash -e\n", "bash -e"},
		{"env with other options", "#!/usr/bin/env -i bash -e\n", "env -i bash -e"},
		{"bare env", "#!/usr/bin/env\n", "php"},
		{"php with -r", "#!/usr/bin/php -r\n", "php -r"},
		{"sh with -c", "#!/bin/sh -c\n", "sh -c"},
		// cmd meta characters and command substitution are refused
		{"cmd metacharacters in an argument", "#!/bin/sh -e & calc.exe\n", "php"},
		{"crlf line ending", "#!/usr/bin/env php\r\n<?php", "php"},
		{"trailing whitespace", "#!/usr/bin/env php  \n", "php"},
		// the kernel only honors a "#!" at the very first byte, so this file
		// has no shebang
		{"leading whitespace", "  #!/bin/sh\n", "php"},
		{"no shebang", "<?php\n", "php"},
		{"cmd metacharacters", "#!/usr/bin/env php\" \" & calc.exe\n", "php"},
		{"command substitution", "#!/usr/bin/env php$(id)\n", "php"},
		{"php open tag", "#!<?php echo 'PWNED'; ?>\n<?php", "php"},
	} {
		if err := os.WriteFile(bin, []byte(tc.contents), 0o644); err != nil {
			t.Fatal(err)
		}

		if got, err := util.DetermineBinaryCaller(bin); err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestBinaryInstaller_DetermineBinaryCallerForBatFiles(t *testing.T) {
	rootDir, _, _ := binaryFixture(t)

	for _, path := range []string{rootDir + "/does-not-exist.bat", rootDir + "/does-not-exist.exe"} {
		if got, err := util.DetermineBinaryCaller(path); err != nil || got != "call" {
			t.Errorf("%s: %q, %v; want call", path, got, err)
		}
	}
}

func TestBinaryInstaller_IsSafeBinPathAcceptsPathsPublishedPackagesUse(t *testing.T) {
	for _, bin := range []string{
		"bin/console",
		"bin/32 bit/wkhtmltopdf.exe",
		`src\yii`,
		"bin/it's",
		"bin/foo-bar.php",
	} {
		if !util.IsSafeBinPath(bin) {
			t.Errorf("IsSafeBinPath(%q) = false", bin)
		}
	}
}

// generateProxy is BinaryInstallerTest::generateProxy: writes out the unixy
// proxy the installer would generate, bypassing InstallBinaries().
func generateProxy(t *testing.T, binDir, bin, link string) string {
	t.Helper()

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.NullString{})

	proxy, err := installer.generateUnixyProxyCode(bin, link)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(link, []byte(proxy), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bin, 0o755); err != nil {
		t.Fatal(err)
	}

	return proxy
}

// TestBinaryInstaller_EscapeShellArg has sh read each escaped word back.
func TestBinaryInstaller_EscapeShellArg(t *testing.T) {
	for _, s := range []string{
		"plain-name",
		`a"b`,
		`a\b`,
		`a\"; touch pwned #`,
		`trailing\`,
		"$(touch pwned)`touch pwned`${HOME}$1",
		"new\nline\t'single'*?[x]",
		"it's",
	} {
		out, err := exec.Command("sh", "-c", "printf %s "+escapeShellArg(s)).Output()
		if err != nil || string(out) != s {
			t.Errorf("%q: read back %q, %v", s, out, err)
		}
	}
}

// TestBinaryInstaller_PhpProxyDoesNotRunCodeInjectedViaTheBinPath: every
// segment of a bin path is package controlled, incl. the directory names.
func TestBinaryInstaller_PhpProxyDoesNotRunCodeInjectedViaTheBinPath(t *testing.T) {
	if _, err := exec.LookPath("php"); err != nil {
		t.Skip("php is not available")
	}

	rootDir, vendorDir, binDir := binaryFixture(t)

	// InstallBinaries() refuses this path outright, so go through the
	// generator directly to keep the escaping itself covered.
	bin := "a*/ namespace Injected; echo 'PWNED'; /* x/binary"
	installPath := vendorDir + "/attacker/pkg"
	mustMkdir(t, installPath+"/"+php.Dirname(bin))

	binFile := installPath + "/" + bin
	if err := os.WriteFile(binFile, []byte("<?php\n\necho 'success '.$_SERVER['argv'][1];"), 0o755); err != nil {
		t.Fatal(err)
	}

	proxy := generateProxy(t, binDir, binFile, binDir+"/binary")

	// the first comment terminator of the proxy must be the one ending its
	// own docblock
	docblockEnd := strings.Index(proxy, "*/")
	if docblockEnd < 0 || !strings.Contains(proxy[:docblockEnd], "@generated") {
		t.Fatalf("the bin path closed the docblock: %q", proxy[:max(0, docblockEnd)])
	}

	cmd := exec.Command(binDir+"/binary", "arg")
	cmd.Dir = rootDir

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	if string(out) != "success arg" {
		t.Errorf("output %q", out)
	}
}

// TestBinaryInstaller_ShProxyDoesNotRunCodeInjectedViaTheBinFilename: the
// bin names are spliced into the shell proxy, so they must not expand there.
func TestBinaryInstaller_ShProxyDoesNotRunCodeInjectedViaTheBinFilename(t *testing.T) {
	names := []string{
		`pwn$(echo INJECTED >&2)`,
		"pwn`echo INJECTED >&2`",
		`pwn";echo INJECTED >&2;"`,
		`pwn'quote`,
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			rootDir, vendorDir, binDir := binaryFixture(t)

			installPath := vendorDir + "/attacker/pkg"
			mustMkdir(t, installPath)

			binFile := installPath + "/" + name
			if err := os.WriteFile(binFile, []byte("#!/bin/sh\nprintf 'success %s' \"$1\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			// InstallBinaries() refuses these filenames outright, so go
			// through the generator directly to keep the escaping covered.
			generateProxy(t, binDir, binFile, binDir+"/"+name)

			cmd := exec.Command("sh", "-c", escapeShellArg(binDir+"/"+name)+" arg")
			cmd.Dir = rootDir

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}

			if string(out) != "success arg" {
				t.Errorf("output %q", out)
			}

			for _, dir := range []string{rootDir, binDir, installPath} {
				if _, err := os.Stat(dir + "/pwned"); err == nil {
					t.Errorf("the bin name ran a command in %s", dir)
				}
			}
		})
	}
}

// TestBinaryInstaller_PhpProxyOnlyCarriesOverAPlainPhpShebang.
func TestBinaryInstaller_PhpProxyOnlyCarriesOverAPlainPhpShebang(t *testing.T) {
	for _, shebang := range []string{
		"#!<?php echo 'PWNED'; ?>",
		`#!/usr/bin/env php" " & calc.exe`,
		"#!/usr/bin/env php$(id)",
		// a non-php interpreter above a "<?php" body can only be there to
		// get the proxy itself handed to it, the body would not run either way
		"#!/bin/sh",
	} {
		t.Run(shebang, func(t *testing.T) {
			_, vendorDir, binDir := binaryFixture(t)

			installPath := vendorDir + "/attacker/pkg"
			mustMkdir(t, installPath)

			if err := os.WriteFile(installPath+"/binary", []byte(shebang+"\n<?php\n\necho 'success';"), 0o644); err != nil {
				t.Fatal(err)
			}

			installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.NullString{})
			if err := installer.InstallBinaries(binPackage("binary"), installPath, true); err != nil {
				t.Fatal(err)
			}

			proxy, err := os.ReadFile(binDir + "/binary")
			if err != nil {
				t.Fatal(err)
			}

			if !strings.HasPrefix(string(proxy), "#!/usr/bin/env php\n") {
				t.Errorf("an unsafe shebang was carried over: %q", proxy[:min(len(proxy), 40)])
			}
			if strings.Contains(string(proxy), shebang) {
				t.Errorf("the proxy contains the shebang %q", shebang)
			}
		})
	}
}

// TestBinaryInstaller_PhpProxyCarriesOverAPhpShebangWithItsArguments.
func TestBinaryInstaller_PhpProxyCarriesOverAPhpShebangWithItsArguments(t *testing.T) {
	for _, shebang := range []string{
		"#!/usr/bin/env php",
		"#!/usr/bin/php8.1 -dmemory_limit=-1",
		"#!/usr/bin/env -S php -d x=1",
	} {
		t.Run(shebang, func(t *testing.T) {
			_, vendorDir, binDir := binaryFixture(t)

			installPath := vendorDir + "/foo/bar"
			mustMkdir(t, installPath)

			if err := os.WriteFile(installPath+"/binary", []byte(shebang+"\n<?php\n\necho 'success';"), 0o644); err != nil {
				t.Fatal(err)
			}

			installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.NullString{})
			if err := installer.InstallBinaries(binPackage("binary"), installPath, true); err != nil {
				t.Fatal(err)
			}

			proxy, err := os.ReadFile(binDir + "/binary")
			if err != nil {
				t.Fatal(err)
			}

			if !strings.HasPrefix(string(proxy), shebang+"\n") {
				t.Errorf("proxy starts with %q, want %q", proxy[:min(len(proxy), len(shebang))], shebang)
			}
		})
	}
}

// TestBinaryInstaller_WindowsProxyEscapesTheTargetPath.
func TestBinaryInstaller_WindowsProxyEscapesTheTargetPath(t *testing.T) {
	_, vendorDir, binDir := binaryFixture(t)

	installPath := vendorDir + "/foo/bar/a&b"
	mustMkdir(t, installPath)

	bin := installPath + "/bin%x"
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nprintf hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})

	bat, err := installer.generateWindowsProxyCode(bin, binDir+"/bin%x.bat")
	if err != nil {
		t.Fatal(err)
	}

	// the quoted SET keeps cmd.exe from acting on the & of the path, and the
	// % is doubled as a batch file collapses %% back to a single %
	for _, want := range []string{
		"SET \"BIN_TARGET=%~dp0/../vendor/foo/bar/a&b/bin%%x\"\r\n",
		"SET \"COMPOSER_RUNTIME_BIN_DIR=%~dp0\"\r\n",
	} {
		if !strings.Contains(bat, want) {
			t.Errorf("bat %q lacks %q", bat, want)
		}
	}
}

// TestBinaryInstaller_WindowsProxyFallsBackToPhpForAnUnsafeShebang.
func TestBinaryInstaller_WindowsProxyFallsBackToPhpForAnUnsafeShebang(t *testing.T) {
	_, vendorDir, binDir := binaryFixture(t)

	installPath := vendorDir + "/attacker/pkg"
	mustMkdir(t, installPath)

	bin := installPath + "/binary"
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env php\" \" & calc.exe\n<?php\n\necho 'success';"), 0o755); err != nil {
		t.Fatal(err)
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})

	bat, err := installer.generateWindowsProxyCode(bin, binDir+"/binary.bat")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(bat, "calc.exe") {
		t.Errorf("bat %q runs the shebang's command", bat)
	}
	if !strings.HasSuffix(bat, "php \"%BIN_TARGET%\" %*\r\n") {
		t.Errorf("bat %q does not run php", bat)
	}
}

// TestBinaryInstaller_WindowsProxyRunsAnEnvSplitShebangWithItsInterpreter.
func TestBinaryInstaller_WindowsProxyRunsAnEnvSplitShebangWithItsInterpreter(t *testing.T) {
	_, vendorDir, binDir := binaryFixture(t)

	installPath := vendorDir + "/foo/bar"
	mustMkdir(t, installPath)

	bin := installPath + "/binary"
	if err := os.WriteFile(bin, []byte("#!/usr/bin/env -S bash -e\necho 'success'"), 0o755); err != nil {
		t.Fatal(err)
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})

	bat, err := installer.generateWindowsProxyCode(bin, binDir+"/binary.bat")
	if err != nil {
		t.Fatal(err)
	}

	if want := "SET \"BIN_TARGET=%~dp0/../vendor/foo/bar/binary\"\r\n"; !strings.Contains(bat, want) {
		t.Errorf("bat %q lacks %q", bat, want)
	}
	if !strings.HasSuffix(bat, "bash -e \"%BIN_TARGET%\" %*\r\n") {
		t.Errorf("bat %q does not run bash -e", bat)
	}
}

// TestBinaryInstaller_WindowsProxyRunsPhpBinsThroughTheUnixyProxy: the unixy
// proxy is what defines _composer_autoload_path, so the .bat must point at it.
func TestBinaryInstaller_WindowsProxyRunsPhpBinsThroughTheUnixyProxy(t *testing.T) {
	for _, shebang := range []string{
		"",
		"#!/usr/bin/env php\n",
		"#!/usr/bin/php7.4\n",
		"#!/usr/bin/env php -d memory_limit=-1\n",
	} {
		t.Run(shebang, func(t *testing.T) {
			_, vendorDir, binDir := binaryFixture(t)

			installPath := vendorDir + "/foo/bar"
			mustMkdir(t, installPath)

			bin := installPath + "/bin%x"
			if err := os.WriteFile(bin, []byte(shebang+"<?php\n\necho 'success';"), 0o755); err != nil {
				t.Fatal(err)
			}

			installer := NewBinaryInstaller(newBufferIO(t), binDir, "full", util.NewFilesystem(nil), pkg.NullString{})

			bat, err := installer.generateWindowsProxyCode(bin, binDir+"/bin%x.bat")
			if err != nil {
				t.Fatal(err)
			}

			if want := "SET \"BIN_TARGET=%~dp0/bin%%x\"\r\n"; !strings.Contains(bat, want) {
				t.Errorf("bat %q lacks %q", bat, want)
			}
		})
	}
}

// TestBinaryInstaller_PhpProxyDocblockCannotBeBrokenOutOfWithALineBreak.
func TestBinaryInstaller_PhpProxyDocblockCannotBeBrokenOutOfWithALineBreak(t *testing.T) {
	_, vendorDir, binDir := binaryFixture(t)

	// the bin path embedded in the docblock is the one relative to the
	// proxy, so it carries the user's own project path too
	installPath := vendorDir + "/foo/li\r\nne/pkg"
	mustMkdir(t, installPath)

	bin := installPath + "/binary"
	if err := os.WriteFile(bin, []byte("<?php\n\necho 'success';"), 0o755); err != nil {
		t.Fatal(err)
	}

	proxy := generateProxy(t, binDir, bin, binDir+"/binary")

	docblock, _, _ := strings.Cut(proxy, "*/")
	if !strings.Contains(docblock, "@generated") {
		t.Errorf("the docblock was closed early: %q", docblock)
	}

	want := "\n * This file includes the referenced bin path (../vendor/foo/li  ne/pkg/binary)\n"
	if !strings.Contains(docblock, want) {
		t.Errorf("docblock %q lacks %q", docblock, want)
	}
}

// TestBinaryInstaller_FullCompatSkipsABinWhosePathCannotBeRepresentedInABatProxy:
// the bin dir comes from the user's own config rather than from the package.
func TestBinaryInstaller_FullCompatSkipsABinWhosePathCannotBeRepresentedInABatProxy(t *testing.T) {
	for _, dirName := range []string{"bin\"dir", "bin\ndir", "bin\x1adir"} {
		t.Run(dirName, func(t *testing.T) {
			rootDir, vendorDir, _ := binaryFixture(t)

			binDir := rootDir + "/" + dirName
			mustMkdir(t, binDir)

			installPath := vendorDir + "/foo/bar"
			mustMkdir(t, installPath)

			if err := os.WriteFile(installPath+"/binary", []byte("#!/bin/sh\nprintf hi\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			io := newBufferIO(t)

			installer := NewBinaryInstaller(io, binDir, "full", util.NewFilesystem(nil), pkg.NullString{})
			if err := installer.InstallBinaries(binPackage("binary"), installPath, true); err != nil {
				t.Fatal(err)
			}

			if out := io.Output(); !strings.Contains(out, "cannot be used in a Windows bin proxy") {
				t.Errorf("output %q lacks the warning", out)
			}

			for _, proxy := range []string{"/binary", "/binary.bat"} {
				if _, err := os.Stat(binDir + proxy); err == nil {
					t.Errorf("%s was created", proxy)
				}
			}
		})
	}
}

// TestBinaryInstaller_InstallBinaryRejectsBinPathWithMetacharacters: bins
// reach BinaryInstaller from sources ValidatingArrayLoader never sees.
func TestBinaryInstaller_InstallBinaryRejectsBinPathWithMetacharacters(t *testing.T) {
	_, vendorDir, binDir := binaryFixture(t)

	installPath := vendorDir + "/attacker/pkg"
	mustMkdir(t, installPath)

	if err := os.WriteFile(installPath+"/pwn$(id)", []byte("#!/bin/sh\nprintf hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.NullString{})
	if err := installer.InstallBinaries(binPackage("pwn$(id)"), installPath, true); err != nil {
		t.Fatal(err)
	}

	if php.FileExists(binDir + "/pwn$(id)") {
		t.Error("no proxy must be created for a bin path with metacharacters")
	}
}
