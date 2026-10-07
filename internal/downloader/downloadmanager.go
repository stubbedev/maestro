// Ports src/Composer/Downloader/DownloadManager.php.

package downloader

import (
	"fmt"
	"slices"
	"strings"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/resolver/operation"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// DownloadManager ports Composer\Downloader\DownloadManager. Downloaders
// are registered by type with SetDownloader, as Composer's Factory does.
type DownloadManager struct {
	io                 mio.IO
	filesystem         *util.Filesystem
	downloaders        map[string]Downloader
	packagePreferences []preference
	// types are the downloader types in registration order.
	types                           []string
	preferDist                      bool
	preferSource                    bool
	sourceFallback                  bool
	sourceFallbackDeprecationWarned bool
}

// preference is one entry of the preferred-install map.
type preference struct {
	pattern    *php.Regexp
	preference any
}

// NewDownloadManager is new DownloadManager($io, $preferSource,
// $filesystem); a nil filesystem is a new one.
func NewDownloadManager(io mio.IO, preferSource bool, filesystem *util.Filesystem) *DownloadManager {
	if filesystem == nil {
		filesystem = util.NewFilesystem(nil)
	}

	return &DownloadManager{io: io, preferSource: preferSource, filesystem: filesystem, downloaders: map[string]Downloader{}}
}

// SetPreferSource is setPreferSource().
func (m *DownloadManager) SetPreferSource(preferSource bool) *DownloadManager {
	m.preferSource = preferSource

	return m
}

// SetPreferDist is setPreferDist().
func (m *DownloadManager) SetPreferDist(preferDist bool) *DownloadManager {
	m.preferDist = preferDist

	return m
}

// SetPreferences is setPreferences(): the preferred-install map of package
// patterns to "dist", "source" or "auto".
func (m *DownloadManager) SetPreferences(preferences *php.Array) (*DownloadManager, error) {
	prefs := make([]preference, 0, preferences.Len())

	for k, v := range preferences.All() {
		re, err := php.Compile("{^" + strings.ReplaceAll(php.PregQuote(k.String(), ""), `\*`, ".*") + "$}i")
		if err != nil {
			return m, err
		}

		prefs = append(prefs, preference{pattern: re, preference: v})
	}

	m.packagePreferences = prefs

	return m, nil
}

// SetSourceFallback is setSourceFallback().
func (m *DownloadManager) SetSourceFallback(sourceFallback bool) *DownloadManager {
	if sourceFallback && !m.sourceFallbackDeprecationWarned {
		m.io.WriteError("<warning>The source-fallback option is deprecated and will be removed in Composer 2.11 as automatic fallback between dist and source has security implications.</warning>", true, mio.Normal)
		m.io.WriteError("<warning>If you have a legitimate use case and do not want us to remove it in 2.11, please open an issue at https://github.com/composer/composer/issues to let us know.</warning>", true, mio.Normal)
		m.sourceFallbackDeprecationWarned = true
	}

	m.sourceFallback = sourceFallback

	return m
}

// SetDownloader is setDownloader(): the downloader for an installation
// type.
func (m *DownloadManager) SetDownloader(typ string, downloader Downloader) *DownloadManager {
	typ = php.Strtolower(typ)
	if _, ok := m.downloaders[typ]; !ok {
		m.types = append(m.types, typ)
	}

	m.downloaders[typ] = downloader

	return m
}

// Downloader is getDownloader(): the downloader for an installation type.
func (m *DownloadManager) Downloader(typ string) (Downloader, error) {
	typ = php.Strtolower(typ)

	d, ok := m.downloaders[typ]
	if !ok {
		return nil, &util.InvalidArgumentError{Message: fmt.Sprintf("Unknown downloader type: %s. Available types: %s.", typ, strings.Join(m.types, ", "))}
	}

	return d, nil
}

// DownloaderForPackage is getDownloaderForPackage(): the downloader of an
// installed package, nil for a metapackage.
func (m *DownloadManager) DownloaderForPackage(p pkg.PackageInterface) (Downloader, error) {
	installationSource := p.InstallationSource()

	if p.Type() == "metapackage" {
		return nil, nil //nolint:nilnil // PHP's null downloader
	}

	var (
		d   Downloader
		err error
	)

	switch {
	case installationSource.Valid && installationSource.S == "dist":
		d, err = m.Downloader(p.DistType().S)
	case installationSource.Valid && installationSource.S == "source":
		d, err = m.Downloader(p.SourceType().S)
	default:
		return nil, &util.InvalidArgumentError{Message: "Package " + p.String() + " does not have an installation source set"}
	}

	if err != nil {
		return nil, err
	}

	if installationSource.S != d.InstallationSource() {
		return nil, &util.LogicError{Message: fmt.Sprintf("Downloader \"%s\" is a %s type downloader and can not be used to download %s for package %s",
			d.PHPClass(), d.InstallationSource(), installationSource.S, p.String())}
	}

	return d, nil
}

// DownloaderType is getDownloaderType(): the type a downloader is
// registered for, false when it is not.
func (m *DownloadManager) DownloaderType(d Downloader) (string, bool) {
	for _, typ := range m.types {
		if m.downloaders[typ] == d {
			return typ, true
		}
	}

	return "", false
}

// Download is download($package, $targetDir, $prevPackage): it downloads
// from the preferred source, falling back to the other one where allowed.
func (m *DownloadManager) Download(p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)
	if err := util.EnsureDirectoryExists(php.Dirname(targetDir)); err != nil {
		return nil, err
	}

	sources, err := m.availableSources(p, prev)
	if err != nil {
		return nil, err
	}

	promise, err := m.download(p, targetDir, prev, &sources, false)

	return promise, err
}

// download is download()'s $download closure.
func (m *DownloadManager) download(p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface, sources *[]string, retry bool) (*Promise, error) {
	source := (*sources)[0]
	*sources = (*sources)[1:]

	if retry {
		m.io.WriteError("    <warning>Now trying to download from "+source+"</warning>", true, mio.Normal)
	}

	p.SetInstallationSource(pkg.Str(source))

	d, err := m.DownloaderForPackage(p)
	if err != nil {
		return nil, err
	}

	if d == nil {
		return resolved(""), nil
	}

	handleError := func(e error) (*Promise, error) {
		if !phperr.InstanceOf(e, "RuntimeException") || isIrrecoverable(e) {
			return nil, e
		}

		nextSource := ""
		if len(*sources) > 0 {
			nextSource = (*sources)[0]
		}

		// Only fallback from dist to source is gated by the sourceFallback
		// flag, as it can silently switch to less-trusted code or cause
		// other issues where people rely on dists. Falling back the other
		// way around (source -> dist) is always allowed.
		blocked := nextSource == "source" && !m.sourceFallback
		if nextSource == "" || blocked {
			if blocked {
				m.io.WriteError("    <warning>Failed to download "+p.PrettyName()+" from "+source+": "+e.Error()+"</warning>", true, mio.Normal)
				m.io.WriteError("    <warning>Source fallback is disabled. Not trying alternative sources.</warning>", true, mio.Normal)
			}

			return nil, e
		}

		m.io.WriteError("    <warning>Failed to download "+p.PrettyName()+" from "+source+": "+e.Error()+"</warning>", true, mio.Normal)

		return m.download(p, targetDir, prev, sources, true)
	}

	result, err := d.Download(p, targetDir, prev)
	if err != nil {
		return handleError(err)
	}

	return then(result, func(res string) (*Promise, string, error) {
		return nil, res, nil
	}, func(e error) (*Promise, string, error) {
		promise, err := handleError(e)

		return promise, "", err
	}), nil
}

// Prepare is prepare($type, $package, $targetDir, $prevPackage).
func (m *DownloadManager) Prepare(typ operation.Type, p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)

	d, err := m.DownloaderForPackage(p)
	if err != nil || d == nil {
		return resolved(""), err
	}

	promise, err := d.Prepare(typ, p, targetDir, prev)

	return promise, err
}

// Install is install($package, $targetDir).
func (m *DownloadManager) Install(p pkg.PackageInterface, targetDir string) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)

	d, err := m.DownloaderForPackage(p)
	if err != nil || d == nil {
		return resolved(""), err
	}

	promise, err := d.Install(p, targetDir)

	return promise, err
}

// Update is update($initial, $target, $targetDir).
func (m *DownloadManager) Update(initial, target pkg.PackageInterface, targetDir string) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)

	d, err := m.DownloaderForPackage(target)
	if err != nil {
		return nil, err
	}

	initialDownloader, err := m.DownloaderForPackage(initial)
	if err != nil {
		return nil, err
	}

	// no downloaders present means update from metapackage to metapackage,
	// nothing to do
	if initialDownloader == nil && d == nil {
		return resolved(""), nil
	}

	// if we have a downloader present before, but not after, the package
	// became a metapackage and its files should be removed
	if d == nil {
		promise, err := initialDownloader.Remove(initial, targetDir)

		return promise, err
	}

	// we had no downloader but now have one, so a metapackage became a
	// concrete package and we just install it
	if initialDownloader == nil {
		promise, err := d.Install(target, targetDir)

		return promise, err
	}

	initialType, initialOK := m.DownloaderType(initialDownloader)
	targetType, targetOK := m.DownloaderType(d)
	sameType := initialType == targetType && initialOK == targetOK

	if sameType {
		promise, err := d.Update(initial, target, targetDir)
		if err == nil {
			return promise, nil
		}

		if !phperr.InstanceOf(err, "RuntimeException") || !m.io.IsInteractive() {
			return nil, err
		}

		m.io.WriteError("<error>    Update failed ("+err.Error()+")</error>", true, mio.Normal)

		reinstall, aerr := m.io.AskConfirmation("    Would you like to try reinstalling the package instead [<comment>yes</comment>]? ", true)
		if aerr != nil {
			return nil, aerr
		}

		if !reinstall {
			return nil, err
		}
	}

	// if downloader type changed, or update failed and user asks for
	// reinstall, we wipe the dir and do a new install instead of updating
	// it
	promise := resolved("")

	if !sameType {
		// on a type change the existing source install is about to be
		// wiped, so run its uninstall guard first to avoid silently
		// dropping local changes in a modified VCS checkout
		if promise, err = initialDownloader.Prepare(operation.TypeUninstall, initial, targetDir, nil); err != nil {
			return nil, err
		}
	}

	promise = then(promise, func(string) (*Promise, string, error) {
		promise, err := initialDownloader.Remove(initial, targetDir)

		return promise, "", err
	}, nil)

	return then(promise, func(string) (*Promise, string, error) {
		promise, err := m.Install(target, targetDir)

		return promise, "", err
	}, nil), nil
}

// Remove is remove($package, $targetDir).
func (m *DownloadManager) Remove(p pkg.PackageInterface, targetDir string) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)

	d, err := m.DownloaderForPackage(p)
	if err != nil || d == nil {
		return resolved(""), err
	}

	promise, err := d.Remove(p, targetDir)

	return promise, err
}

// Cleanup is cleanup($type, $package, $targetDir, $prevPackage).
func (m *DownloadManager) Cleanup(typ operation.Type, p pkg.PackageInterface, targetDir string, prev pkg.PackageInterface) (*Promise, error) {
	targetDir = normalizeTargetDir(targetDir)

	d, err := m.DownloaderForPackage(p)
	if err != nil || d == nil {
		return resolved(""), err
	}

	promise, err := d.Cleanup(typ, p, targetDir, prev)

	return promise, err
}

// ResolvePackageInstallPreference is resolvePackageInstallPreference() for
// the plugin shim (a subclass's call of the protected method).
func (m *DownloadManager) ResolvePackageInstallPreference(p pkg.PackageInterface) (string, error) {
	return m.resolvePackageInstallPreference(p)
}

// resolvePackageInstallPreference is resolvePackageInstallPreference():
// "dist" or "source", or the PcreException Preg::isMatch throws.
func (m *DownloadManager) resolvePackageInstallPreference(p pkg.PackageInterface) (string, error) {
	for _, pref := range m.packagePreferences {
		ok, err := pref.pattern.IsMatch(p.Name())
		if err != nil {
			return "", err
		}
		if ok {
			if pref.preference == "dist" || (!p.IsDev() && pref.preference == "auto") {
				return "dist", nil
			}

			return "source", nil
		}
	}

	if p.IsDev() {
		return "source", nil
	}

	return "dist", nil
}

// availableSources is getAvailableSources().
func (m *DownloadManager) availableSources(p, prev pkg.PackageInterface) ([]string, error) {
	sourceType := p.SourceType()
	distType := p.DistType()

	// add source before dist by default
	sources := make([]string, 0, 2)
	if php.ToBool(sourceType.Value()) {
		sources = append(sources, "source")
	}

	if php.ToBool(distType.Value()) {
		sources = append(sources, "dist")
	}

	if len(sources) == 0 {
		return nil, &util.InvalidArgumentError{Message: "Package " + p.String() + " must have a source or dist specified"}
	}

	if prev != nil {
		prevSource := prev.InstallationSource()

		// if we are updating, we want to keep the same source as the
		// previously installed package (if available in the new one) unless
		// the previous package was stable dist (by default) and the new
		// package is dev, then we allow the new default to take over
		if prevSource.Valid && slices.Contains(sources, prevSource.S) && (prev.IsDev() || prevSource.S != "dist" || !p.IsDev()) {
			if sources[0] != prevSource.S {
				slices.Reverse(sources)
			}

			return sources, nil
		}
	}

	// reverse sources in case dist is the preferred source for this package
	if !m.preferSource {
		preferDist := m.preferDist
		if !preferDist {
			preference, err := m.resolvePackageInstallPreference(p)
			if err != nil {
				return nil, err
			}
			preferDist = preference == "dist"
		}
		if preferDist {
			slices.Reverse(sources)
		}
	}

	return sources, nil
}

// normalizeTargetDir is normalizeTargetDir(): downloaders expect a
// /path/to/dir without trailing slash.
func normalizeTargetDir(dir string) string {
	if dir == `\` || dir == "/" {
		return dir
	}

	return strings.TrimRight(dir, `\/`)
}

// syncDownloader adapts a DownloadManager to SyncHelper.
type syncDownloader struct{ m *DownloadManager }

// Sync returns the manager as SyncHelper takes it
// (http.DownloadAndInstallPackageSync).
func (m *DownloadManager) Sync() http.SyncDownloader[pkg.PackageInterface] { return syncDownloader{m} }

func waitable(p *Promise, err error) (http.Waitable, error) {
	if p == nil {
		return nil, err
	}

	return p, err
}

func (s syncDownloader) Download(p pkg.PackageInterface, path string, prev pkg.PackageInterface) (http.Waitable, error) {
	return waitable(s.m.Download(p, path, prev))
}

func (s syncDownloader) Prepare(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (http.Waitable, error) {
	return waitable(s.m.Prepare(typ, p, path, prev))
}

func (s syncDownloader) Install(p pkg.PackageInterface, path string) (http.Waitable, error) {
	return waitable(s.m.Install(p, path))
}

func (s syncDownloader) Update(p, prev pkg.PackageInterface, path string) (http.Waitable, error) {
	return waitable(s.m.Update(p, prev, path))
}

func (s syncDownloader) Cleanup(typ operation.Type, p pkg.PackageInterface, path string, prev pkg.PackageInterface) (http.Waitable, error) {
	return waitable(s.m.Cleanup(typ, p, path, prev))
}
