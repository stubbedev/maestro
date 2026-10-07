// Ports src/Composer/Installer/BinaryInstaller.php.

package installer

import (
	"io/fs"
	"os"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/store"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// Binaries is what LibraryInstaller asks of its BinaryInstaller; a PHP
// BinaryInstaller passed by a plugin implements it too.
type Binaries interface {
	// InstallBinaries is installBinaries($package, $installPath,
	// $warnOnOverwrite).
	InstallBinaries(p pkg.PackageInterface, installPath string, warnOnOverwrite bool) error
	// RemoveBinaries is removeBinaries($package).
	RemoveBinaries(p pkg.PackageInterface) error
}

// BinaryInstaller ports Composer\Installer\BinaryInstaller: the vendor/bin
// proxies of package binaries.
type BinaryInstaller struct {
	binDir     string
	binCompat  string
	io         mio.IO
	filesystem *util.Filesystem
	// vendorDir is null when a custom installer did not set one.
	vendorDir pkg.NullString
}

var _ Binaries = (*BinaryInstaller)(nil)

// NewBinaryInstaller is new BinaryInstaller($io, $binDir, $binCompat,
// $filesystem, $vendorDir); a nil filesystem is a new one.
func NewBinaryInstaller(io mio.IO, binDir, binCompat string, filesystem *util.Filesystem, vendorDir pkg.NullString) *BinaryInstaller {
	if filesystem == nil {
		filesystem = util.NewFilesystem(nil)
	}

	return &BinaryInstaller{binDir: binDir, binCompat: binCompat, io: io, filesystem: filesystem, vendorDir: vendorDir}
}

// BinDir is the $binDir property (realpath'd once binaries were handled).
func (b *BinaryInstaller) BinDir() string { return b.binDir }

// InstallBinaries is installBinaries().
func (b *BinaryInstaller) InstallBinaries(p pkg.PackageInterface, installPath string, warnOnOverwrite bool) error {
	binaries := binariesOf(p)
	if len(binaries) == 0 {
		return nil
	}

	util.WorkaroundFilesystemIssues()

	for _, bin := range binaries {
		binPath := installPath + "/" + bin
		if !php.FileExists(binPath) {
			b.io.WriteError("    <warning>Skipped installation of bin "+bin+" for package "+p.Name()+": file not found in package</warning>", true, mio.Normal)

			continue
		}

		if php.IsDir(binPath) {
			b.io.WriteError("    <warning>Skipped installation of bin "+bin+" for package "+p.Name()+": found a directory at that path</warning>", true, mio.Normal)

			continue
		}

		// A malicious package can pass the ".." bin metadata check yet ship
		// the bin as a symlink pointing outside the package (e.g. to
		// ../../../victim.sh), following it here would let the package
		// chmod/proxy an arbitrary host file (GHSA-96h3-5x6v-m776).
		if !util.IsBinPathInsidePackage(installPath, binPath) {
			b.io.WriteError("    <warning>Skipped installation of bin "+bin+" for package "+p.Name()+": the bin resolves to a path outside of the package directory</warning>", true, mio.Normal)

			continue
		}

		if !fspath.IsAbsolutePath(binPath) {
			// in case a custom installer returned a relative path for the
			// $package, we can now safely turn it into a absolute path (as
			// we already checked the binary's existence). The following
			// helpers will require absolute paths to work properly.
			binPath, _ = php.Realpath(binPath)
		}

		if err := b.initializeBinDir(); err != nil {
			return err
		}

		link := b.binDir + "/" + php.Basename(bin, "")
		if php.FileExists(link) {
			if !isLink(link) {
				if warnOnOverwrite {
					b.io.WriteError("    Skipped installation of bin "+bin+" for package "+p.Name()+": name conflicts with an existing file", true, mio.Normal)
				}

				continue
			}

			if php.RealpathString(link) == php.RealpathString(binPath) {
				// It is a linked binary from a previous installation, which
				// can be replaced with a proxy file
				if err := util.Unlink(link); err != nil {
					return err
				}
			}
		}

		binCompat := b.binCompat
		if binCompat == "auto" && (util.IsWindows() || util.IsWindowsSubsystemForLinux()) {
			binCompat = "full"
		}

		var err error
		if binCompat == "full" {
			err = b.installFullBinaries(binPath, link, bin, p)
		} else {
			err = b.installUnixyProxyBinaries(binPath, link)
		}

		if err != nil {
			return err
		}

		silentChmod(binPath)
	}

	return nil
}

// RemoveBinaries is removeBinaries().
//
// Deliberate deviation (docs/PORTING.md 7): Composer calls initializeBinDir()
// before checking for binaries, so removing a package without binaries
// creates an empty bin dir, and whether it survives a batch of removals
// depends on the order the removals finish. maestro only touches the bin
// dir for packages that have binaries.
func (b *BinaryInstaller) RemoveBinaries(p pkg.PackageInterface) error {
	binaries := binariesOf(p)
	if len(binaries) == 0 {
		return nil
	}

	if err := b.initializeBinDir(); err != nil {
		return err
	}

	for _, bin := range binaries {
		link := b.binDir + "/" + php.Basename(bin, "")
		if isLink(link) || php.FileExists(link) { // still checking for symlinks here for legacy support
			if err := util.Unlink(link); err != nil {
				return err
			}
		}

		if php.IsFile(link + ".bat") {
			if err := util.Unlink(link + ".bat"); err != nil {
				return err
			}
		}
	}

	// attempt removing the bin dir in case it is left empty
	if php.IsDir(b.binDir) {
		if empty, _ := util.IsDirEmpty(b.binDir); empty {
			_ = os.Remove(b.binDir)
		}
	}

	return nil
}

// binariesOf is getBinaries(): the package's bin entries.
func binariesOf(p pkg.PackageInterface) []string {
	bins := p.Binaries()
	if bins == nil {
		return nil
	}

	out := make([]string, 0, bins.Len())
	for _, v := range bins.All() {
		out = append(out, php.ToString(v))
	}

	return out
}

func (b *BinaryInstaller) installFullBinaries(binPath, link, bin string, p pkg.PackageInterface) error {
	// add unixy support for cygwin and similar environments
	if !strings.HasSuffix(binPath, ".bat") {
		if err := b.installUnixyProxyBinaries(binPath, link); err != nil {
			return err
		}

		link += ".bat"
		if php.FileExists(link) {
			b.io.WriteError("    Skipped installation of bin "+bin+".bat proxy for package "+p.Name()+": a .bat proxy was already installed", true, mio.Normal)
		}
	}

	if !php.FileExists(link) {
		code, err := b.generateWindowsProxyCode(binPath, link)
		if err != nil {
			return err
		}

		if err := filePutContents(link, code); err != nil {
			return err
		}

		silentChmod(link)
	}

	return nil
}

func (b *BinaryInstaller) installUnixyProxyBinaries(binPath, link string) error {
	code, err := b.generateUnixyProxyCode(binPath, link)
	if err != nil {
		return err
	}

	if err := filePutContents(link, code); err != nil {
		return err
	}

	silentChmod(link)

	return nil
}

func (b *BinaryInstaller) initializeBinDir() error {
	if err := util.EnsureDirectoryExists(b.binDir); err != nil {
		return err
	}

	b.binDir = php.RealpathString(b.binDir)

	return nil
}

func (b *BinaryInstaller) generateWindowsProxyCode(bin, link string) (string, error) {
	binPath, err := util.FindShortestPath(link, bin, false, false)
	if err != nil {
		return "", err
	}

	caller, err := util.DetermineBinaryCaller(bin)
	if err != nil {
		return "", err
	}

	// if the target is a php file, we run the unixy proxy file to ensure
	// that _composer_autoload_path gets defined, instead of running the
	// binary directly
	target := binPath
	if caller == "php" {
		target = php.Basename(link, ".bat")
	}

	return "@ECHO OFF\r\n" +
		"setlocal DISABLEDELAYEDEXPANSION\r\n" +
		"SET BIN_TARGET=%~dp0/" + php.TrimSet(util.Escape(target), `"'`) + "\r\n" +
		"SET COMPOSER_RUNTIME_BIN_DIR=%~dp0\r\n" +
		caller + " \"%BIN_TARGET%\" %*\r\n", nil
}

// phpBinPattern finds PHP files (with an optional shebang) among the bins.
var phpBinPattern = php.MustCompile(`{^(#!.*\r?\n)?[\r\n\t ]*<\?php}`)

func (b *BinaryInstaller) generateUnixyProxyCode(bin, link string) (string, error) {
	binPath, err := util.FindShortestPath(link, bin, false, false)
	if err != nil {
		return "", err
	}

	binDir := util.Escape(php.Dirname(binPath))
	binFile := php.Basename(binPath, "")

	binContents, err := fileGetContents(bin, 500)
	if err != nil {
		return "", err
	}

	// For php files, we generate a PHP proxy instead of a shell one, which
	// allows calling the proxy with a custom php process
	match, err := phpBinPattern.Match(binContents)
	if err != nil {
		return "", err
	}

	if match == nil {
		return shellProxyCode(binDir, binFile), nil
	}

	// carry over the existing shebang if present, otherwise add our own
	proxyCode := "#!/usr/bin/env php"
	if shebang, ok := match.Group(1); ok {
		proxyCode = php.Trim(shebang)
	}

	binPathExported, err := util.FindShortestPathCode(link, bin, false, true, false)
	if err != nil {
		return "", err
	}

	streamProxy, streamHint := "", ""
	globalsCode := "$GLOBALS['_composer_bin_dir'] = __DIR__;\n"
	phpunitHack1, phpunitHack2 := "", ""

	// Don't expose autoload path when vendor dir was not set in custom
	// installers
	if b.vendorDir.Valid {
		// ensure comparisons work accurately if the CWD is a symlink, as
		// $link is realpath'd already
		vendorDirReal, ok := php.Realpath(b.vendorDir.S)
		if !ok {
			vendorDirReal = b.vendorDir.S
		}

		autoloadPath, err := util.FindShortestPathCode(link, vendorDirReal+"/autoload.php", false, true, false)
		if err != nil {
			return "", err
		}

		globalsCode += "$GLOBALS['_composer_autoload_path'] = " + autoloadPath + ";\n"
	}

	// Add workaround for PHPUnit process isolation
	if fspath.NormalizePath(bin) == fspath.NormalizePath(b.vendorDir.S+"/phpunit/phpunit/phpunit") {
		// workaround issue on PHPUnit 6.5+ running on PHP 8+
		globalsCode += "$GLOBALS['__PHPUNIT_ISOLATION_EXCLUDE_LIST'] = $GLOBALS['__PHPUNIT_ISOLATION_BLACKLIST'] = array(realpath(" + binPathExported + "));\n"
		// workaround issue on all PHPUnit versions running on PHP <8
		phpunitHack1 = "'phpvfscomposer://'."
		phpunitHack2 = "\n                $data = str_replace('__DIR__', var_export(dirname($this->realpath), true), $data);\n                $data = str_replace('__FILE__', var_export($this->realpath, true), $data);"
	}

	if php.Trim(match.Get(0)) != "<?php" {
		streamHint = " using a stream wrapper to prevent the shebang from being output on PHP<8\n *"
		streamProxy = streamProxyCode(phpunitHack1, phpunitHack2, binPathExported)
	}

	return proxyCode + "\n" + phpProxyCode(binPathExported, binPath, streamHint, globalsCode, streamProxy), nil
}

// silentChmod is Silencer::call('chmod', $path, 0777 & ~umask()), through
// the store: a package file hard-linked to a store object is unshared first,
// so that the mode changes in this project only.
func silentChmod(path string) {
	_ = store.Chmod(path, 0o777&^store.Umask())
}

// filePutContents is file_put_contents() under Composer's error handler.
func filePutContents(path, data string) error {
	if err := os.WriteFile(path, []byte(data), 0o666); err != nil {
		return &util.ErrorException{Message: "file_put_contents(" + path + "): Failed to open stream: " + php.Strerror(err)}
	}

	return nil
}

// fileGetContents is (string) file_get_contents($path, false, null, 0,
// $length) under Composer's error handler.
func fileGetContents(path string, length int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", &util.ErrorException{Message: "file_get_contents(" + path + "): Failed to open stream: " + php.Strerror(err)}
	}

	defer func() { _ = f.Close() }()

	buf := make([]byte, length)
	n := 0

	for n < length {
		m, err := f.Read(buf[n:])
		n += m

		if err != nil {
			break
		}
	}

	return string(buf[:n]), nil
}

// isLink is is_link().
func isLink(path string) bool {
	st, err := os.Lstat(path)

	return err == nil && st.Mode()&fs.ModeSymlink != 0
}

// GenerateUnixyProxyCode is the protected generateUnixyProxyCode() (a
// subclass's parent call, in the plugin shim).
func (b *BinaryInstaller) GenerateUnixyProxyCode(bin, link string) (string, error) {
	return b.generateUnixyProxyCode(bin, link)
}

// GenerateWindowsProxyCode is the protected generateWindowsProxyCode().
func (b *BinaryInstaller) GenerateWindowsProxyCode(bin, link string) (string, error) {
	return b.generateWindowsProxyCode(bin, link)
}

// InstallFullBinaries is the protected installFullBinaries().
func (b *BinaryInstaller) InstallFullBinaries(binPath, link, bin string, p pkg.PackageInterface) error {
	return b.installFullBinaries(binPath, link, bin, p)
}

// InstallUnixyProxyBinaries is the protected installUnixyProxyBinaries().
func (b *BinaryInstaller) InstallUnixyProxyBinaries(binPath, link string) error {
	return b.installUnixyProxyBinaries(binPath, link)
}
