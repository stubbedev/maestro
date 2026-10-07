// Ports src/Composer/Repository/PlatformRepository.php.

package repository

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/platform"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
)

// Versions of Composer's own platform packages: Composer::VERSION,
// PluginInterface::PLUGIN_API_VERSION and Composer::RUNTIME_API_VERSION
// of the Composer release maestro reproduces.
const (
	ComposerVersion   = "2.10.3"
	PluginAPIVersion  = "2.9.0"
	RuntimeAPIVersion = "2.2.2"
)

// PlatformPackageRegex is PlatformRepository::PLATFORM_PACKAGE_REGEX;
// IsPlatformPackage matches it.
const PlatformPackageRegex = pkg.PlatformPackageRegex

// IsPlatformPackage ports PlatformRepository::isPlatformPackage.
func IsPlatformPackage(name string) bool { return pkg.IsPlatformPackage(name) }

// platformOverride is an entry of PlatformRepository::$overrides; a
// disabled package has no version (PHP's false).
type platformOverride struct {
	name     string
	version  string
	disabled bool
}

// PlatformRepository ports Composer\Repository\PlatformRepository: the
// packages of the platform (php, its extensions and libraries, Composer's
// own APIs), as the Runtime reports them, with config.platform overrides.
type PlatformRepository struct {
	ArrayRepository
	versionParser *pkg.VersionParser
	// overrides is keyed by lowercased package name.
	overrides        *NameMap[platformOverride]
	disabledPackages *NameMap[pkg.CompletePackageInterface]
	runtime          platform.Runtime
	hhvmDetector     platform.HhvmVersionDetector
	skippedXdebug    string
	// lastSeenPlatformPhp is PHP's static $lastSeenPlatformPhp.
	lastSeenPlatformPhp string
}

var _ pkg.PlatformRepositoryMarker = (*PlatformRepository)(nil)

// PlatformOptions are PlatformRepository's collaborators.
type PlatformOptions struct {
	// Runtime answers the questions about the PHP Composer runs on; nil
	// when there is no php, which only works when config.platform
	// overrides php (the other platform packages are then missing).
	Runtime platform.Runtime
	// HhvmDetector finds HHVM; nil finds none.
	HhvmDetector platform.HhvmVersionDetector
	// SkippedXdebugVersion is XdebugHandler::getSkippedVersion(): the
	// xdebug version bin/composer restarted without
	// (platform.Snapshot.ComposerView).
	SkippedXdebugVersion string
}

// NewPlatformRepository ports new PlatformRepository($packages, $overrides,
// $runtime, $hhvmDetector). overrides is config.platform: package name =>
// version string, or false to disable the package.
func NewPlatformRepository(packages []pkg.PackageInterface, overrides *php.Array, opts PlatformOptions) (*PlatformRepository, error) {
	r := &PlatformRepository{
		overrides:        &NameMap[platformOverride]{},
		disabledPackages: &NameMap[pkg.CompletePackageInterface]{},
		runtime:          opts.Runtime,
		hhvmDetector:     opts.HhvmDetector,
		skippedXdebug:    opts.SkippedXdebugVersion,
		versionParser:    pkg.NewVersionParser(),
	}
	r.bind(r, r)
	if overrides != nil {
		for key, version := range overrides.All() {
			name := key.String()
			o := platformOverride{name: name}
			switch v := version.(type) {
			case string:
				o.version = v
			case bool:
				if v {
					return nil, &util.UnexpectedValueError{Message: "config.platform." + name + " should be a string or false, but got bool true"}
				}
				if name == "php" {
					return nil, &util.UnexpectedValueError{Message: "config.platform." + name + " cannot be set to false as you cannot disable php entirely."}
				}
				o.disabled = true
			default:
				return nil, &util.UnexpectedValueError{Message: "config.platform." + name + " should be a string or false, but got " + php.TypeName(version) + " " + php.VarExport(version)}
			}
			r.overrides.Set(php.Strtolower(name), o)
		}
	}
	if err := r.addPackages(packages); err != nil {
		return nil, err
	}

	return r, nil
}

// PHPClass returns the PHP class name.
func (r *PlatformRepository) PHPClass() string { return `Composer\Repository\PlatformRepository` }

// IsPlatformRepository implements pkg.PlatformRepositoryMarker.
func (r *PlatformRepository) IsPlatformRepository() bool { return true }

// RepoName ports PlatformRepository::getRepoName.
func (r *PlatformRepository) RepoName() string { return "platform repo" }

// IsPlatformPackageDisabled ports isPlatformPackageDisabled.
func (r *PlatformRepository) IsPlatformPackageDisabled(name string) bool {
	return r.disabledPackages.Has(name)
}

// DisabledPackages ports getDisabledPackages: the packages disabled via
// config.platform, with their actual version.
func (r *PlatformRepository) DisabledPackages() *NameMap[pkg.CompletePackageInterface] {
	return r.disabledPackages
}

// PlatformPhpVersion ports PlatformRepository::getPlatformPhpVersion: the
// config.platform.php version ("major.minor.patch" of the normalized
// version) once the repository was initialized; ok false is null. Composer
// keeps it in a process-wide static; here it is the repository's.
func (r *PlatformRepository) PlatformPhpVersion() (string, bool) {
	return r.lastSeenPlatformPhp, r.lastSeenPlatformPhp != ""
}

// Search ports PlatformRepository::search: vendor searches find nothing.
func (r *PlatformRepository) Search(query string, mode int, typ string) ([]SearchResult, error) {
	// suppress vendor search as there are no vendors to match in platform packages
	if mode == SearchVendor {
		return nil, nil
	}

	return r.ArrayRepository.Search(query, mode, typ)
}

// platformLibraries is initialize()'s $libraries: the lib-* names added.
type platformLibraries map[string]struct{}

// initialize ports PlatformRepository::initialize.
func (r *PlatformRepository) initialize() error {
	r.baseInitialize()

	libraries := platformLibraries{}

	// Add each of the override versions as options.
	// Later we might even replace the extensions instead.
	for _, override := range r.overrides.All() {
		// Check that it's a platform package.
		if !IsPlatformPackage(override.name) {
			return &util.InvalidArgumentError{Message: "Invalid platform package name in config.platform: " + override.name}
		}

		if !override.disabled {
			if _, err := r.addOverriddenPackage(override, ""); err != nil {
				return err
			}
		}
	}

	for _, p := range [...]struct{ name, prettyVersion, description string }{
		{"composer", ComposerVersion, "Composer package"},
		{"composer-plugin-api", PluginAPIVersion, "The Composer Plugin API"},
		{"composer-runtime-api", RuntimeAPIVersion, "The Composer Runtime API"},
	} {
		if err := r.addCompletePackage(p.name, p.prettyVersion, p.description); err != nil {
			return err
		}
	}

	if r.runtime == nil {
		if r.overrides.Has("php") {
			return nil
		}

		return &platform.PHPNotFoundError{Purpose: "platform detection"}
	}

	if err := r.addPhpPackages(); err != nil {
		return err
	}

	loadedExtensions := r.runtime.GetExtensions()

	// Extensions scanning
	for _, name := range loadedExtensions {
		if name == "standard" || name == "Core" {
			continue
		}

		if err := r.addExtension(name, r.runtime.GetExtensionVersion(name)); err != nil {
			return err
		}
	}

	// Check for Xdebug in a restarted process
	if !slices.Contains(loadedExtensions, "xdebug") && r.skippedXdebug != "" {
		if err := r.addExtension("xdebug", r.skippedXdebug); err != nil {
			return err
		}
	}

	// Another quick loop, just for possible libraries
	// Doing it this way to know that functions or constants exist before
	// relying on them.
	for _, name := range loadedExtensions {
		if err := r.addExtensionLibraries(libraries, name, loadedExtensions); err != nil {
			return err
		}
	}

	if r.hhvmDetector != nil {
		if hhvmVersion := r.hhvmDetector.GetVersion(); hhvmVersion != "" {
			prettyVersion, version, err := r.normalizeRuntimeVersion(hhvmVersion)
			if err != nil {
				return err
			}
			hhvm := pkg.NewCompletePackage("hhvm", version, prettyVersion)
			hhvm.SetDescription(pkg.Str("The HHVM Runtime (64bit)"))
			if err := r.hooks.addPackage(hhvm); err != nil {
				return err
			}
		}
	}

	return nil
}

var runtimeVersionPrefix = php.MustCompile(`#^([^~+-]+).*$#`)

// normalizeRuntimeVersion normalizes the PHP or HHVM version, falling back
// on its part before any ~, + or - when it does not normalize.
func (r *PlatformRepository) normalizeRuntimeVersion(prettyVersion string) (string, string, error) {
	version, err := r.versionParser.Normalize(prettyVersion)
	if isUnexpectedValue(err) {
		prettyVersion, _, err = runtimeVersionPrefix.Replace(prettyVersion, "$1", -1)
		if err != nil {
			return "", "", err
		}
		version, err = r.versionParser.Normalize(prettyVersion)
	}

	return prettyVersion, version, err
}

func (r *PlatformRepository) addCompletePackage(name, prettyVersion, description string) error {
	version, err := r.versionParser.Normalize(prettyVersion)
	if err != nil {
		return err
	}
	p := pkg.NewCompletePackage(name, version, prettyVersion)
	p.SetDescription(pkg.Str(description))

	return r.hooks.addPackage(p)
}

// constantString is $this->runtime->getConstant($name, $class) where a
// ?string is expected: anything but a string or null is the TypeError
// strict_types raises in the addLibrary() call.
func (r *PlatformRepository) constantString(name, class string) (pkg.NullString, error) {
	v, err := r.runtime.GetConstant(name, class)
	if err != nil {
		return pkg.NullString{}, err
	}

	return nullableString(v)
}

// nullableString checks that v can be passed as addLibrary()'s ?string
// $prettyVersion.
func nullableString(v any) (pkg.NullString, error) {
	switch v := v.(type) {
	case nil:
		return pkg.NullString{}, nil
	case string:
		return pkg.Str(v), nil
	}

	return pkg.NullString{}, pkg.ArgumentTypeError(`Composer\Repository\PlatformRepository::addLibrary`, 3, "prettyVersion", "?string", v)
}

// addPhpPackages adds php and its flavours.
func (r *PlatformRepository) addPhpPackages() error {
	phpVersion, err := r.runtime.GetConstant("PHP_VERSION", "")
	if err != nil {
		return err
	}
	phpVersionString, ok := phpVersion.(string)
	if !ok {
		return pkg.ArgumentTypeError(`Composer\Package\Version\VersionParser::normalize`, 1, "version", "string", phpVersion)
	}
	prettyVersion, version, err := r.normalizeRuntimeVersion(phpVersionString)
	if err != nil {
		return err
	}

	add := func(name, description string) error {
		p := pkg.NewCompletePackage(name, version, prettyVersion)
		p.SetDescription(pkg.Str(description))

		return r.hooks.addPackage(p)
	}
	if err := add("php", "The PHP interpreter"); err != nil {
		return err
	}

	debug, err := r.runtime.GetConstant("PHP_DEBUG", "")
	if err != nil {
		return err
	}
	if php.ToBool(debug) {
		if err := add("php-debug", "The PHP interpreter, with debugging symbols"); err != nil {
			return err
		}
	}

	if r.runtime.HasConstant("PHP_ZTS", "") {
		zts, err := r.runtime.GetConstant("PHP_ZTS", "")
		if err != nil {
			return err
		}
		if php.ToBool(zts) {
			if err := add("php-zts", "The PHP interpreter, with Zend Thread Safety"); err != nil {
				return err
			}
		}
	}

	intSize, err := r.runtime.GetConstant("PHP_INT_SIZE", "")
	if err != nil {
		return err
	}
	if intSize == int64(8) {
		if err := add("php-64bit", "The PHP interpreter, 64bit"); err != nil {
			return err
		}
	}

	// The AF_INET6 constant is only defined if ext-sockets is available but
	// IPv6 support might still be available.
	ipv6 := r.runtime.HasConstant("AF_INET6", "")
	if !ipv6 {
		// Silencer::call([$this->runtime, 'invoke'], ...) at line 180
		// calls it at Silencer.php:67 (and catches only \Exception)
		v, err := r.runtime.Invoke(platform.Func("inet_pton"), "::")
		if err != nil {
			return err
		}
		ipv6 = v != false
	}
	if ipv6 {
		if err := add("php-ipv6", "The PHP interpreter, with IPv6 support"); err != nil {
			return err
		}
	}

	return nil
}

// extensionInfo is $this->runtime->getExtensionInfo($name).
func (r *PlatformRepository) extensionInfo(name string) (string, error) {
	return r.runtime.GetExtensionInfo(name)
}

// matchNamed matches pattern against subject: the named group (strict
// groups variants fail when it did not participate, as an unmatched
// group cannot be used).
// The error is the PcreException Preg throws.
func matchNamed(pattern *php.Regexp, subject string, groups ...string) (map[string]string, bool, error) {
	m, err := pattern.Match(subject)
	if err != nil || m == nil {
		return nil, false, err
	}
	values := make(map[string]string, len(groups))
	for _, g := range groups {
		v, ok := m.Named(g)
		if !ok {
			return nil, false, nil
		}
		values[g] = v
	}

	return values, true, nil
}

var (
	reAmqpLibrabbitmq   = php.MustCompile(`/^librabbitmq version => (?<version>.+)$/im`)
	reAmqpProtocol      = php.MustCompile(`/^AMQP protocol version => (?<version>.+)$/im`)
	reBz2               = php.MustCompile(`/^BZip2 Version => (?<version>.*),/im`)
	reCurlSSL           = php.MustCompile(`{^SSL Version => (?<library>[^\r\n/]+)/(?<version>[^\r\n]+?)\r?$}im`)
	reSecureTransport   = php.MustCompile(`{^\(securetransport\) ([a-z0-9]+)}`)
	reCurlSSH           = php.MustCompile(`{^libSSH Version => (?<library>[^\r\n/]+)/(?<version>.+?)(?:/.*)?$}im`)
	reCurlZlib          = php.MustCompile(`{^ZLib Version => (?<version>.+)$}im`)
	reTimelib           = php.MustCompile(`/^timelib version => (?<version>.+)$/im`)
	reZoneinfoSource    = php.MustCompile(`/^Timezone Database => (?<source>internal|external)$/im`)
	reZoneinfoVersion   = php.MustCompile(`/^"Olson" Timezone Database Version => (?<version>.+?)(?:\.system)?$/im`)
	reLibmagic          = php.MustCompile(`/^libmagic => (?<version>.+)$/im`)
	reLibjpeg           = php.MustCompile(`/^libJPEG Version => (?<version>.+?)(?: compatible)?$/im`)
	reLibpng            = php.MustCompile(`/^libPNG Version => (?<version>.+)$/im`)
	reFreetype          = php.MustCompile(`/^FreeType Version => (?<version>.+)$/im`)
	reLibxpm            = php.MustCompile(`/^libXpm Version => (?<versionId>\d+)$/im`)
	reICU               = php.MustCompile(`/^ICU version => (?<version>.+)$/im`)
	reICUTZData         = php.MustCompile(`/^ICU TZData version => (?<version>.*)$/im`)
	reImageMagick       = php.MustCompile(`/^ImageMagick (?<version>[\d.]+)(?:-(?<patch>\d+))?/`)
	reLdapVendorVersion = php.MustCompile(`/^Vendor Version => (?<versionId>\d+)$/im`)
	reLdapVendorName    = php.MustCompile(`/^Vendor Name => (?<vendor>.+)$/im`)
	reLibmbfl           = php.MustCompile(`/^libmbfl version => (?<version>.+)$/im`)
	reOniguruma         = php.MustCompile(`/^(?:oniguruma|Multibyte regex \(oniguruma\)) version => (?<version>.+)$/im`)
	reLibmemcached      = php.MustCompile(`/^libmemcached version => (?<version>.+)$/im`)
	reOpenssl           = php.MustCompile(`{^(?:OpenSSL|LibreSSL)?\s*(?<version>\S+)}i`)
	rePcreVersion       = php.MustCompile(`{^(\S+).*}`)
	rePcreUnicode       = php.MustCompile(`/^PCRE Unicode Version => (?<version>.+)$/im`)
	reMysqlnd           = php.MustCompile(`/^(?:Client API version|Version) => mysqlnd (?<version>.+?) /mi`)
	reLibmongoc         = php.MustCompile(`/^libmongoc bundled version => (?<version>.+)$/im`)
	reLibbson           = php.MustCompile(`/^libbson bundled version => (?<version>.+)$/im`)
	reLibpq             = php.MustCompile(`/^PostgreSQL\(libpq\) Version => (?<version>.*)$/im`)
	rePqLibpq           = php.MustCompile(`/^libpq => (?<compiled>.+) => (?<linked>.+)$/im`)
	reSqlite            = php.MustCompile(`/^SQLite Library => (?<version>.+)$/im`)
	reLibssh2           = php.MustCompile(`/^libssh2 version => (?<version>.+)$/im`)
	reLibxsltLibxml     = php.MustCompile(`/^libxslt compiled against libxml Version => (?<version>.+)$/im`)
	reLibyaml           = php.MustCompile(`/^LibYAML Version => (?<version>.+)$/im`)
	reZlibLinked        = php.MustCompile(`/^Linked Version => (?<version>.+)$/im`)
)

// libraryFromInfo adds the library whose version the extension info of
// name holds in the group "version" of pattern.
func (r *PlatformRepository) libraryFromInfo(libraries platformLibraries, info string, pattern *php.Regexp, libName, description string) error {
	m, ok, err := matchNamed(pattern, info, "version")
	if err != nil {
		return err
	}
	if ok {
		return r.addLibrary(libraries, libName, pkg.Str(m["version"]), description, nil, nil)
	}

	return nil
}

// addExtensionLibraries is the switch of initialize()'s library loop.
func (r *PlatformRepository) addExtensionLibraries(libraries platformLibraries, name string, loadedExtensions []string) error {
	switch name {
	case "amqp":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// librabbitmq version => 0.9.0
		if err := r.libraryFromInfo(libraries, info, reAmqpLibrabbitmq, name+"-librabbitmq", "AMQP librabbitmq version"); err != nil {
			return err
		}

		// AMQP protocol version => 0-9-1
		m, ok, err := matchNamed(reAmqpProtocol, info, "version")
		if err != nil {
			return err
		}
		if ok {
			return r.addLibrary(libraries, name+"-protocol", pkg.Str(strings.ReplaceAll(m["version"], "-", ".")), "AMQP protocol version", nil, nil)
		}

	case "bz2":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// BZip2 Version => 1.0.6, 6-Sept-2010
		return r.libraryFromInfo(libraries, info, reBz2, name, "")

	case "curl":
		curlVersion, err := r.runtime.Invoke(platform.Func("curl_version"))
		if err != nil {
			return err
		}
		var version any
		if a, ok := curlVersion.(*php.Array); ok {
			version, _ = a.Get("version")
		}
		v, err := nullableString(version)
		if err != nil {
			return err
		}
		if err := r.addLibrary(libraries, name, v, "", nil, nil); err != nil {
			return err
		}

		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// SSL Version => OpenSSL/1.0.1t
		// unversioned backends (=> Schannel) are not reported, the library must not span lines (#12615)
		m, ok, err := matchNamed(reCurlSSL, info, "library", "version")
		if err != nil {
			return err
		}
		if ok {
			library := php.Strtolower(m["library"])
			if library == "openssl" {
				parsedVersion, isFips, ok, err := platform.ParseOpenssl(m["version"])
				if err != nil {
					return err
				}
				libName, provides := name+"-openssl", []string(nil)
				if isFips {
					libName, provides = name+"-openssl-fips", []string{"curl-openssl"}
				}
				if err := r.addLibrary(libraries, libName, pkg.NullString{S: parsedVersion, Valid: ok}, "curl OpenSSL version ("+parsedVersion+")", nil, provides); err != nil {
					return err
				}
			} else {
				shortlib, sslLib := library, "curl-openssl"
				if strings.HasPrefix(library, "(securetransport)") {
					sm, err := reSecureTransport.Match(library)
					if err != nil {
						return err
					}
					if sm != nil {
						shortlib, sslLib = "securetransport", "curl-"+sm.Get(1)
					}
				}
				if err := r.addLibrary(libraries, name+"-"+shortlib, pkg.Str(m["version"]), "curl "+library+" version ("+m["version"]+")", []string{sslLib}, nil); err != nil {
					return err
				}
			}
		}

		// libSSH Version => libssh2/1.4.3
		m, ok, err = matchNamed(reCurlSSH, info, "library", "version")
		if err != nil {
			return err
		}
		if ok {
			if err := r.addLibrary(libraries, name+"-"+php.Strtolower(m["library"]), pkg.Str(m["version"]), "curl "+m["library"]+" version", nil, nil); err != nil {
				return err
			}
		}

		// ZLib Version => 1.2.8
		return r.libraryFromInfo(libraries, info, reCurlZlib, name+"-zlib", "curl zlib version")

	case "date":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// timelib version => 2018.03
		if err := r.libraryFromInfo(libraries, info, reTimelib, name+"-timelib", "date timelib version"); err != nil {
			return err
		}

		// Timezone Database => internal
		m, ok, err := matchNamed(reZoneinfoSource, info, "source")
		if err != nil {
			return err
		}
		if ok {
			external := m["source"] == "external"
			zm, ok, err := matchNamed(reZoneinfoVersion, info, "version")
			if err != nil {
				return err
			}
			if ok {
				// If the timezonedb is provided by ext/timezonedb, register that version as a replacement
				if external && slices.Contains(loadedExtensions, "timezonedb") {
					return r.addLibrary(libraries, "timezonedb-zoneinfo", pkg.Str(zm["version"]), `zoneinfo ("Olson") database for date (replaced by timezonedb)`, []string{name + "-zoneinfo"}, nil)
				}

				return r.addLibrary(libraries, name+"-zoneinfo", pkg.Str(zm["version"]), `zoneinfo ("Olson") database for date`, nil, nil)
			}
		}

	case "fileinfo":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// libmagic => 537
		return r.libraryFromInfo(libraries, info, reLibmagic, name+"-libmagic", "fileinfo libmagic version")

	case "gd":
		v, err := r.constantString("GD_VERSION", "")
		if err != nil {
			return err
		}
		if err := r.addLibrary(libraries, name, v, "", nil, nil); err != nil {
			return err
		}

		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		m, ok, err := matchNamed(reLibjpeg, info, "version")
		if err != nil {
			return err
		}
		if ok {
			libjpeg, ok, err := platform.ParseLibjpeg(m["version"])
			if err != nil {
				return err
			}
			if err := r.addLibrary(libraries, name+"-libjpeg", pkg.NullString{S: libjpeg, Valid: ok}, "libjpeg version for gd", nil, nil); err != nil {
				return err
			}
		}

		if err := r.libraryFromInfo(libraries, info, reLibpng, name+"-libpng", "libpng version for gd"); err != nil {
			return err
		}

		if err := r.libraryFromInfo(libraries, info, reFreetype, name+"-freetype", "freetype version for gd"); err != nil {
			return err
		}

		m, ok, err = matchNamed(reLibxpm, info, "versionId")
		if err != nil {
			return err
		}
		if ok {
			return r.addLibrary(libraries, name+"-libxpm", pkg.Str(platform.ConvertLibxpmVersionId(php.ToInt(m["versionId"]))), "libxpm version for gd", nil, nil)
		}

	case "gmp":
		return r.addConstantLibrary(libraries, name, "GMP_VERSION", "")

	case "iconv":
		return r.addConstantLibrary(libraries, name, "ICONV_VERSION", "")

	case "intl":
		return r.addIntlLibraries(libraries, name)

	case "imagick":
		object, err := r.runtime.Construct("Imagick")
		if err != nil {
			return err
		}
		imagick, ok := object.(platform.Imagick)
		if !ok {
			return &util.ErrorException{Message: "Call to a member function getVersion() on " + php.TypeName(object)}
		}
		imageMagickVersion, err := imagick.GetVersion()
		if err != nil {
			return err
		}
		var versionString string
		if a, ok := imageMagickVersion.(*php.Array); ok {
			versionString = php.ToString(a.At("versionString"))
		}
		// 6.x: ImageMagick 6.2.9 08/24/06 Q16 http://www.imagemagick.org
		// 7.x: ImageMagick 7.0.8-34 Q16 x86_64 2019-03-23 https://imagemagick.org
		m, err := reImageMagick.Match(versionString)
		if err != nil {
			return err
		}
		if m != nil {
			version, _ := m.Named("version")
			if patch, ok := m.Named("patch"); ok {
				version += "." + patch
			}

			return r.addLibrary(libraries, name+"-imagemagick", pkg.Str(version), "", []string{"imagick"}, nil)
		}

	case "ldap":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		m, ok, err := matchNamed(reLdapVendorVersion, info, "versionId")
		if err != nil {
			return err
		}
		if ok {
			vm, ok, err := matchNamed(reLdapVendorName, info, "vendor")
			if err != nil {
				return err
			}
			if ok {
				return r.addLibrary(libraries, name+"-"+php.Strtolower(vm["vendor"]), pkg.Str(platform.ConvertOpenldapVersionId(php.ToInt(m["versionId"]))), vm["vendor"]+" version of ldap", nil, nil)
			}
		}

	case "libxml":
		// ext/dom, ext/simplexml, ext/xmlreader and ext/xmlwriter use the same libxml as the ext/libxml
		var libxmlProvides []string
		for _, extension := range loadedExtensions {
			switch extension {
			case "dom", "simplexml", "xml", "xmlreader", "xmlwriter":
				libxmlProvides = append(libxmlProvides, extension+"-libxml")
			}
		}
		v, err := r.constantString("LIBXML_DOTTED_VERSION", "")
		if err != nil {
			return err
		}

		return r.addLibrary(libraries, name, v, "libxml library version", nil, libxmlProvides)

	case "mbstring":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// libmbfl version => 1.3.2
		if err := r.libraryFromInfo(libraries, info, reLibmbfl, name+"-libmbfl", "mbstring libmbfl version"); err != nil {
			return err
		}

		versionID, err := r.runtime.GetConstant("PHP_VERSION_ID", "")
		if err != nil {
			return err
		}
		if php.ToInt(versionID) < 90000 && r.runtime.HasConstant("MB_ONIGURUMA_VERSION", "") {
			v, err := r.constantString("MB_ONIGURUMA_VERSION", "")
			if err != nil {
				return err
			}

			return r.addLibrary(libraries, name+"-oniguruma", v, "mbstring oniguruma version", nil, nil)
		}

		// Multibyte regex (oniguruma) version => 5.9.5
		// oniguruma version => 6.9.0
		return r.libraryFromInfo(libraries, info, reOniguruma, name+"-oniguruma", "mbstring oniguruma version")

	case "memcached":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// libmemcached version => 1.0.18
		return r.libraryFromInfo(libraries, info, reLibmemcached, name+"-libmemcached", "libmemcached version")

	case "openssl":
		text, err := r.constantString("OPENSSL_VERSION_TEXT", "")
		if err != nil {
			return err
		}
		// OpenSSL 1.1.1g  21 Apr 2020
		m, ok, err := matchNamed(reOpenssl, text.S, "version")
		if err != nil {
			return err
		}
		if ok {
			parsedVersion, isFips, ok, err := platform.ParseOpenssl(m["version"])
			if err != nil {
				return err
			}
			libName, provides := name, []string(nil)
			if isFips {
				libName, provides = name+"-fips", []string{name}
			}

			return r.addLibrary(libraries, libName, pkg.NullString{S: parsedVersion, Valid: ok}, text.S, nil, provides)
		}

	case "pcre":
		v, err := r.constantString("PCRE_VERSION", "")
		if err != nil {
			return err
		}
		pcreVersion, _, err := rePcreVersion.Replace(v.S, "$1", -1)
		if err != nil {
			return err
		}
		if err := r.addLibrary(libraries, name, pkg.Str(pcreVersion), "", nil, nil); err != nil {
			return err
		}

		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// PCRE Unicode Version => 12.1.0
		return r.libraryFromInfo(libraries, info, rePcreUnicode, name+"-unicode", "PCRE Unicode version support")

	case "mysqlnd", "pdo_mysql":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reMysqlnd, name+"-mysqlnd", "mysqlnd library version for "+name)

	case "mongodb":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		if err := r.libraryFromInfo(libraries, info, reLibmongoc, name+"-libmongoc", "libmongoc version of mongodb"); err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reLibbson, name+"-libbson", "libbson version of mongodb")

	case "pgsql", "pdo_pgsql":
		if name == "pgsql" && r.runtime.HasConstant("PGSQL_LIBPQ_VERSION", "") {
			return r.addConstantLibrary(libraries, "pgsql-libpq", "PGSQL_LIBPQ_VERSION", "libpq for pgsql")
		}
		// intentional fall-through to next case...
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reLibpq, name+"-libpq", "libpq for "+name)

	case "pq":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		// Used Library => Compiled => Linked
		// libpq => 14.3 (Ubuntu 14.3-1.pgdg22.04+1) => 15.0.2
		m, ok, err := matchNamed(rePqLibpq, info, "linked")
		if err != nil {
			return err
		}
		if ok {
			return r.addLibrary(libraries, name+"-libpq", pkg.Str(m["linked"]), "libpq for "+name, nil, nil)
		}

	case "rdkafka":
		if r.runtime.HasConstant("RD_KAFKA_VERSION", "") {
			/**
			 * Interpreted as hex \c MM.mm.rr.xx:
			 *  - MM = Major
			 *  - mm = minor
			 *  - rr = revision
			 *  - xx = pre-release id (0xff is the final release)
			 *
			 * pre-release ID in practice is always 0xff even for RCs etc, so we ignore it
			 */
			v, err := r.runtime.GetConstant("RD_KAFKA_VERSION", "")
			if err != nil {
				return err
			}
			n := php.ToInt(v)
			version := strconv.FormatInt((n&0x7F000000)>>24, 10) + "." + strconv.FormatInt((n&0x00FF0000)>>16, 10) + "." + strconv.FormatInt((n&0x0000FF00)>>8, 10)

			return r.addLibrary(libraries, name+"-librdkafka", pkg.Str(version), "librdkafka for "+name, nil, nil)
		}

	case "libsodium", "sodium":
		if r.runtime.HasConstant("SODIUM_LIBRARY_VERSION", "") {
			if err := r.addConstantLibrary(libraries, "libsodium", "SODIUM_LIBRARY_VERSION", ""); err != nil {
				return err
			}

			return r.addConstantLibrary(libraries, "libsodium", "SODIUM_LIBRARY_VERSION", "")
		}

	case "sqlite3", "pdo_sqlite":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reSqlite, name+"-sqlite", "")

	case "ssh2":
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reLibssh2, name+"-libssh2", "")

	case "xsl":
		v, err := r.constantString("LIBXSLT_DOTTED_VERSION", "")
		if err != nil {
			return err
		}
		if err := r.addLibrary(libraries, "libxslt", v, "", []string{"xsl"}, nil); err != nil {
			return err
		}

		info, err := r.extensionInfo("xsl")
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reLibxsltLibxml, "libxslt-libxml", "libxml version libxslt is compiled against")

	case "yaml":
		info, err := r.extensionInfo("yaml")
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reLibyaml, name+"-libyaml", "libyaml version of yaml")

	case "zip":
		if r.runtime.HasConstant("LIBZIP_VERSION", "ZipArchive") {
			v, err := r.constantString("LIBZIP_VERSION", "ZipArchive")
			if err != nil {
				return err
			}

			return r.addLibrary(libraries, name+"-libzip", v, "", []string{"zip"}, nil)
		}

	case "zlib":
		if r.runtime.HasConstant("ZLIB_VERSION", "") {
			return r.addConstantLibrary(libraries, name, "ZLIB_VERSION", "")
		}

		// Linked Version => 1.2.8
		info, err := r.extensionInfo(name)
		if err != nil {
			return err
		}

		return r.libraryFromInfo(libraries, info, reZlibLinked, name, "")
	}

	return nil
}

// addIntlLibraries is the "intl" case of initialize()'s library loop.
func (r *PlatformRepository) addIntlLibraries(libraries platformLibraries, name string) error {
	info, err := r.extensionInfo(name)
	if err != nil {
		return err
	}

	description := "The ICU unicode and globalization support library"
	// Truthy check is for testing only so we can make the condition fail
	if r.runtime.HasConstant("INTL_ICU_VERSION", "") {
		if err := r.addConstantLibrary(libraries, "icu", "INTL_ICU_VERSION", description); err != nil {
			return err
		}
	} else if err := r.libraryFromInfo(libraries, info, reICU, "icu", description); err != nil {
		return err
	}

	// ICU TZData version => 2019c
	m, ok, err := matchNamed(reICUTZData, info, "version")
	if err != nil {
		return err
	}
	if ok {
		if version, ok, err := platform.ParseZoneinfoVersion(m["version"]); err != nil {
			return err
		} else if ok {
			if err := r.addLibrary(libraries, "icu-zoneinfo", pkg.Str(version), `zoneinfo ("Olson") database for icu`, nil, nil); err != nil {
				return err
			}
		}
	}

	// Add a separate version for the CLDR library version
	if r.runtime.HasClass("ResourceBundle") {
		resourceBundle, err := r.runtime.Invoke(platform.StaticMethod("ResourceBundle", "create"), "root", "ICUDATA", false)
		if err != nil {
			return err
		}
		if resourceBundle != nil {
			bundle, ok := resourceBundle.(platform.ResourceBundle)
			if !ok {
				return &util.ErrorException{Message: "Call to a member function get() on " + php.TypeName(resourceBundle)}
			}
			version, err := bundle.Get("Version")
			if err != nil {
				return err
			}
			v, err := nullableString(version)
			if err != nil {
				return err
			}
			if err := r.addLibrary(libraries, "icu-cldr", v, "ICU CLDR project version", nil, nil); err != nil {
				return err
			}
		}
	}

	if r.runtime.HasClass("IntlChar") {
		unicodeVersion, err := r.runtime.Invoke(platform.StaticMethod("IntlChar", "getUnicodeVersion"))
		if err != nil {
			return err
		}
		parts, ok := unicodeVersion.(*php.Array)
		if !ok {
			return pkg.ArgumentTypeError("array_slice", 1, "array", "array", unicodeVersion)
		}
		var version []string
		for _, v := range php.ArraySlice(parts, 0, 3, false).All() {
			version = append(version, php.ToString(v))
		}

		return r.addLibrary(libraries, "icu-unicode", pkg.Str(strings.Join(version, ".")), "ICU unicode version", nil, nil)
	}

	return nil
}

// addConstantLibrary adds the library whose version is the constant.
func (r *PlatformRepository) addConstantLibrary(libraries platformLibraries, libName, constant, description string) error {
	v, err := r.constantString(constant, "")
	if err != nil {
		return err
	}

	return r.addLibrary(libraries, libName, v, description, nil, nil)
}

// addPackage ports PlatformRepository::addPackage: overridden packages
// are skipped (or disabled), with their actual version noted in the
// overriding package's description.
func (r *PlatformRepository) addPackage(p pkg.PackageInterface) error {
	complete, ok := p.(pkg.CompletePackageInterface)
	if _, isComplete := pkg.AsCompletePackage(p); !ok || !isComplete {
		return &util.UnexpectedValueError{Message: "Expected CompletePackage but got " + p.PHPClass()}
	}

	// Skip if overridden
	if override, ok := r.overrides.Get(p.Name()); ok {
		if override.disabled {
			r.addDisabledPackage(complete)

			return nil
		}

		overrider, err := r.FindPackage(p.Name(), nil)
		if err != nil || overrider == nil {
			return err
		}
		actualText := "actual: " + p.PrettyVersion()
		if p.Version() == overrider.Version() {
			actualText = "same as actual"
		}
		if c, ok := overrider.(pkg.CompletePackageInterface); ok {
			c.SetDescription(pkg.Str(c.Description().S + ", " + actualText))
		}

		return nil
	}

	// Skip if PHP is overridden and we are adding a php-* package
	if override, ok := r.overrides.Get("php"); ok && strings.HasPrefix(p.Name(), "php-") {
		overrider, err := r.addOverriddenPackage(override, p.PrettyName())
		if err != nil {
			return err
		}
		actualText := "actual: " + p.PrettyVersion()
		if p.Version() == overrider.Version() {
			actualText = "same as actual"
		}
		overrider.SetDescription(pkg.Str(overrider.Description().S + ", " + actualText))

		return nil
	}

	return r.addPackageBase(p)
}

// addOverriddenPackage ports PlatformRepository::addOverriddenPackage.
func (r *PlatformRepository) addOverriddenPackage(override platformOverride, name string) (*pkg.CompletePackage, error) {
	version, err := r.versionParser.Normalize(override.version)
	if err != nil {
		return nil, err
	}
	if name == "" {
		name = override.name
	}
	p := pkg.NewCompletePackage(name, version, override.version)
	p.SetDescription(pkg.Str("Package overridden via config.platform"))
	p.SetExtra(php.ArrayOf("config.platform", true))
	if err := r.addPackageBase(p); err != nil {
		return nil, err
	}

	if p.Name() == "php" {
		parts := strings.Split(p.Version(), ".")
		r.lastSeenPlatformPhp = strings.Join(parts[:min(3, len(parts))], ".")
	}

	return p, nil
}

// addDisabledPackage ports PlatformRepository::addDisabledPackage.
func (r *PlatformRepository) addDisabledPackage(p pkg.CompletePackageInterface) {
	p.SetDescription(pkg.Str(p.Description().S + ". <warning>Package disabled via config.platform</warning>"))
	if c, ok := pkg.AsCompletePackage(p); ok {
		c.SetExtra(php.ArrayOf("config.platform", true))
	}

	r.disabledPackages.Set(p.Name(), p)
}

// addExtension ports PlatformRepository::addExtension: the ext- package
// of a loaded extension.
func (r *PlatformRepository) addExtension(name, prettyVersion string) error {
	extraDescription := ""

	version, err := r.versionParser.Normalize(prettyVersion)
	if isUnexpectedValue(err) {
		extraDescription = " (actual version: " + prettyVersion + ")"
		m, merr := reExtensionVersion.Match(prettyVersion)
		if merr != nil {
			return merr
		}
		if m != nil {
			prettyVersion = m.Get(1)
		} else {
			prettyVersion = "0"
		}
		version, err = r.versionParser.Normalize(prettyVersion)
	}
	if err != nil {
		return err
	}

	packageName := buildExtensionPackageName(name)
	ext := pkg.NewCompletePackage(packageName, version, prettyVersion)
	ext.SetDescription(pkg.Str("The " + name + " PHP extension" + extraDescription))
	ext.SetType("php-ext")

	if name == "uuid" {
		var replaces pkg.LinksBuilder
		replaces.Set("lib-uuid", pkg.NewLink("ext-uuid", "lib-uuid", semver.NewConstraintOp(semver.OpEQ, version), pkg.TypeReplace, pkg.Str(ext.PrettyVersion())))
		ext.SetReplaces(replaces.Build())
	}

	return r.hooks.addPackage(ext)
}

var reExtensionVersion = php.MustCompile(`{^(\d+\.\d+\.\d+(?:\.\d+)?)}`)

// buildExtensionPackageName ports PlatformRepository::buildPackageName.
func buildExtensionPackageName(name string) string {
	return "ext-" + strings.ReplaceAll(php.Strtolower(name), " ", "-")
}

// addLibrary ports PlatformRepository::addLibrary: the lib- package of a
// library, unless its version does not normalize, its name is not a
// platform package name, or it was added already. An empty description
// is the default one.
func (r *PlatformRepository) addLibrary(libraries platformLibraries, name string, prettyVersion pkg.NullString, description string, replaces, provides []string) error {
	if !prettyVersion.Valid {
		return nil
	}
	version, err := r.versionParser.Normalize(prettyVersion.S)
	if err != nil {
		if isUnexpectedValue(err) {
			return nil
		}

		return err
	}

	// a parsing glitch in one of the extension info parsers above must not result in a bogus
	// package name, as that would then be looked up as if it was a real package, see #12615
	if !IsPlatformPackage("lib-" + name) {
		return nil
	}

	// avoid adding the same lib twice even if two conflicting extensions provide the same lib
	// see https://github.com/composer/composer/issues/12082
	if _, ok := libraries["lib-"+name]; ok {
		return nil
	}
	libraries["lib-"+name] = struct{}{}

	if description == "" {
		description = "The " + name + " library"
	}

	lib := pkg.NewCompletePackage("lib-"+name, version, prettyVersion.S)
	lib.SetDescription(pkg.Str(description))

	var replaceLinks, provideLinks pkg.LinksBuilder
	for _, replace := range replaces {
		replace = php.Strtolower(replace)
		replaceLinks.Set(replace, pkg.NewLink("lib-"+name, "lib-"+replace, semver.NewConstraintOp(semver.OpEQ, version), pkg.TypeReplace, pkg.Str(lib.PrettyVersion())))
	}
	for _, provide := range provides {
		provide = php.Strtolower(provide)
		provideLinks.Set(provide, pkg.NewLink("lib-"+name, "lib-"+provide, semver.NewConstraintOp(semver.OpEQ, version), pkg.TypeProvide, pkg.Str(lib.PrettyVersion())))
	}
	lib.SetReplaces(replaceLinks.Build())
	lib.SetProvides(provideLinks.Build())

	return r.hooks.addPackage(lib)
}
