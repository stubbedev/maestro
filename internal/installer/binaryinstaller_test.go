//go:build unix

package installer

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
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
	dir := t.TempDir()

	for _, tc := range []struct{ name, contents, want string }{
		{"a.bat", "", "call"},
		{"a.exe", "", "call"},
		{"php", "<?php echo 1;", "php"},
		{"env", "#!/usr/bin/env bash\necho", "bash"},
		{"sh", "#!/bin/sh\necho", "sh"},
		{"node", "#!/usr/local/bin/node  \n", "node"},
		{"empty", "", "php"},
	} {
		path := dir + "/" + tc.name
		if err := os.WriteFile(path, []byte(tc.contents), 0o644); err != nil {
			t.Fatal(err)
		}

		if got, err := util.DetermineBinaryCaller(path); err != nil || got != tc.want {
			t.Errorf("%s: %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

// TestShDoubleQuoted has sh read each escaped string back inside double
// quotes. (A bin name with a backslash cannot run through a proxy: the
// proxy's ${self%[/\\]*} takes backslashes for Windows separators.)
func TestShDoubleQuoted(t *testing.T) {
	for _, s := range []string{
		"plain-name",
		`a"b`,
		`a\b`,
		`a\"; touch pwned #`,
		`trailing\`,
		"$(touch pwned)`touch pwned`${HOME}$1",
		"new\nline\t'single'*?[x]",
	} {
		out, err := exec.Command("sh", "-c", `printf %s "`+shDoubleQuoted(s)+`"`).Output()
		if err != nil || string(out) != s {
			t.Errorf("%q: read back %q, %v", s, out, err)
		}
	}

	if got := shDoubleQuoted("tool-1.0_x"); got != "tool-1.0_x" {
		t.Errorf("an ordinary name changed to %q", got)
	}
}

// TestBinaryInstaller_ShellProxyQuotesHostileBinNames runs the shell proxy
// of bins whose names would end or expand inside its double-quoted
// "${dir}/<name>": each must exec exactly that file and run nothing else.
func TestBinaryInstaller_ShellProxyQuotesHostileBinNames(t *testing.T) {
	names := []string{
		`quote"; touch pwned; "`,
		`dollar$(touch pwned)`,
		"backtick`touch pwned`",
		`var$HOME`,
		"newline\n; touch pwned",
		`star*; touch pwned`,
	}

	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			rootDir, vendorDir, binDir := binaryFixture(t)

			mustMkdir(t, vendorDir+"/foo/bar")

			if err := os.WriteFile(vendorDir+"/foo/bar/"+name, []byte("#!/bin/sh\nprintf 'ran %s' \"$1\"\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			installer := NewBinaryInstaller(newBufferIO(t), binDir, "proxy", util.NewFilesystem(nil), pkg.NullString{})
			if err := installer.InstallBinaries(binPackage(name), vendorDir+"/foo/bar", true); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command("sh", binDir+"/"+name, "arg")
			cmd.Dir = rootDir

			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%v: %s", err, out)
			}

			if string(out) != "ran arg" {
				t.Errorf("output %q", out)
			}

			for _, dir := range []string{rootDir, binDir, vendorDir + "/foo/bar"} {
				if _, err := os.Stat(dir + "/pwned"); err == nil {
					t.Errorf("the bin name ran a command in %s", dir)
				}
			}
		})
	}
}
