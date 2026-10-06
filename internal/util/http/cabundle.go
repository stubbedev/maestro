// Ports composer/ca-bundle's src/CaBundle.php (the version Composer 2.10.3
// locks): finding the system's CA bundle and validating CA files.

package http

import (
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"sync"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// bundledCaCert is composer/ca-bundle's res/cacert.pem (Mozilla's CA
// certificates), the last resort when the system has no bundle.
//
//go:embed res/cacert.pem
var bundledCaCert []byte

// Logger is the PSR-3 logger CaBundle reports to; io.IO implements it.
type Logger interface {
	Debug(message string, context *php.Array)
}

// caBundle holds CaBundle's static caches.
var caBundle struct {
	mu       sync.Mutex
	caPath   string
	found    bool
	validity map[string]bool
}

// caBundleLocations are the well-known bundle locations CaBundle probes
// after the environment.
var caBundleLocations = []string{
	"/etc/pki/ca-trust/extracted/pem/tls-ca-bundle.pem", // Fedora, RHEL, CentOS (ca-certificates package) - NEW
	"/etc/pki/tls/certs/ca-bundle.crt",                  // Fedora, RHEL, CentOS (ca-certificates package) - Deprecated
	"/etc/ssl/certs/ca-certificates.crt",                // Debian, Ubuntu, Gentoo, Arch Linux (ca-certificates package)
	"/etc/ssl/ca-bundle.pem",                            // SUSE, openSUSE (ca-certificates package)
	"/usr/ssl/certs/ca-bundle.crt",                      // Cygwin
	"/opt/local/share/curl/curl-ca-bundle.crt",          // OS X macports, curl-ca-bundle package
	"/usr/local/share/curl/curl-ca-bundle.crt",          // Default cURL CA bunde path (without --with-ca-bundle option)
	"/usr/share/ssl/certs/ca-bundle.crt",                // Really old RedHat?
	"/etc/ssl/cert.pem",                                 // OpenBSD
	"/usr/local/etc/openssl/cert.pem",                   // OS X homebrew, openssl package
	"/usr/local/etc/openssl@1.1/cert.pem",               // OS X homebrew, openssl@1.1 package
	"/opt/homebrew/etc/openssl@3/cert.pem",              // macOS silicon homebrew, openssl@3 package
	"/opt/homebrew/etc/openssl@1.1/cert.pem",            // macOS silicon homebrew, openssl@1.1 package
	"/etc/pki/tls/certs",
	"/etc/ssl/certs", // FreeBSD
}

// iniGet is ini_get() in the PHP Composer runs on (SetIniSource); nil,
// and an unknown setting, are false.
var iniGet struct {
	mu sync.RWMutex
	fn func(name string) (string, bool)
}

// SetIniSource sets where ini_get() reads php.ini settings from: the probed
// php Composer would run on (platform.Snapshot.IniGet of its Composer
// view). ini settings are process-wide in PHP, so the source is too; nil
// means no php (every setting false).
func SetIniSource(fn func(name string) (string, bool)) {
	iniGet.mu.Lock()
	iniGet.fn = fn
	iniGet.mu.Unlock()
}

// phpIniGet is ini_get($name) as a string, "" for false.
func phpIniGet(name string) string {
	iniGet.mu.RLock()
	fn := iniGet.fn
	iniGet.mu.RUnlock()

	if fn == nil {
		return ""
	}

	v, _ := fn(name)

	return v
}

// SystemCaRootBundlePath is CaBundle::getSystemCaRootBundlePath: the CA
// bundle file or directory to verify peers against, found once per
// process. logger may be nil.
func SystemCaRootBundlePath(logger Logger) (string, error) {
	caBundle.mu.Lock()
	defer caBundle.mu.Unlock()

	if caBundle.found {
		return caBundle.caPath, nil
	}

	paths := make([]string, 0, 4+len(caBundleLocations))
	for _, name := range [2]string{"SSL_CERT_FILE", "SSL_CERT_DIR"} {
		v, _ := util.GetEnv(name)
		paths = append(paths, v)
	}

	// $caBundlePaths[] = ini_get('openssl.cafile'); ini_get('openssl.capath')
	paths = append(paths, phpIniGet("openssl.cafile"), phpIniGet("openssl.capath"))

	paths = append(paths, caBundleLocations...)

	for _, caBundlePath := range paths {
		// `if ($caBundle && ...)`: "" and "0" are falsy
		if caBundlePath != "" && caBundlePath != "0" && (caFileUsable(caBundlePath, logger) || caDirUsable(caBundlePath, logger)) {
			caBundle.caPath, caBundle.found = caBundlePath, true

			return caBundlePath, nil
		}
	}

	path, err := BundledCaBundlePath()
	if err != nil {
		return "", err
	}

	caBundle.caPath, caBundle.found = path, true

	return path, nil
}

// BundledCaBundlePath is CaBundle::getBundledCaBundlePath: the embedded
// bundle, written once into maestro's cache directory (as the phar build
// of Composer writes it to a temporary file).
func BundledCaBundlePath() (string, error) {
	sum := sha256.Sum256(bundledCaCert)
	path := filepath.Join(cache.Dir(), "cacert-"+hex.EncodeToString(sum[:8])+".pem")

	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(bundledCaCert)) {
		return path, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o777); err == nil {
		tmp, err := os.CreateTemp(filepath.Dir(path), "cacert-*.tmp")
		if err == nil {
			_, werr := tmp.Write(bundledCaCert)
			cerr := tmp.Close()

			if werr == nil && cerr == nil && os.Rename(tmp.Name(), path) == nil {
				return path, nil
			}

			_ = os.Remove(tmp.Name())
		}
	}

	tmp, err := os.CreateTemp("", "openssl-ca-bundle-")
	if err != nil {
		return "", &util.RuntimeError{Message: "Could not create a temporary file to store the bundled CA file", Site: phperr.At("CaBundle.php", 132)}
	}

	_, werr := tmp.Write(bundledCaCert)
	if cerr := tmp.Close(); werr != nil || cerr != nil {
		return "", &util.RuntimeError{Message: "Could not create a temporary file to store the bundled CA file", Site: phperr.At("CaBundle.php", 132)}
	}

	return tmp.Name(), nil
}

// ValidateCaFile is CaBundle::validateCaFile: whether filename holds a
// parseable certificate (the first one, as openssl_x509_parse reads it).
// Results are cached per file name.
func ValidateCaFile(filename string, logger Logger) bool {
	caBundle.mu.Lock()
	defer caBundle.mu.Unlock()

	return validateCaFile(filename, logger)
}

func validateCaFile(filename string, logger Logger) bool {
	if v, ok := caBundle.validity[filename]; ok {
		return v
	}

	contents, err := os.ReadFile(filename)
	isValid := err == nil && len(contents) > 0 && firstCertificateParses(contents)

	if logger != nil {
		state := "invalid"
		if isValid {
			state = "valid"
		}

		logger.Debug("Checked CA file "+util.Realpath(filename)+": "+state, nil)
	}

	if caBundle.validity == nil {
		caBundle.validity = map[string]bool{}
	}

	caBundle.validity[filename] = isValid

	return isValid
}

// firstCertificateParses is (bool) openssl_x509_parse($contents) after
// CaBundle turned TRUSTED CERTIFICATE markers into plain ones:
// PEM_read_bio_X509 skips other PEM blocks up to the first certificate,
// and d2i_X509 ignores whatever follows the certificate's DER structure
// (such as OpenSSL's trust settings).
func firstCertificateParses(contents []byte) bool {
	for {
		var block *pem.Block

		block, contents = pem.Decode(contents)
		if block == nil {
			return false
		}

		switch block.Type {
		case "CERTIFICATE", "X509 CERTIFICATE", "TRUSTED CERTIFICATE":
			var raw asn1.RawValue

			rest, err := asn1.Unmarshal(block.Bytes, &raw)
			if err != nil {
				return false
			}

			_, err = x509.ParseCertificate(block.Bytes[:len(block.Bytes)-len(rest)])

			return err == nil
		}
	}
}

// caChecked reports whether preparing a request with these ssl options
// would only reuse CaBundle's process-wide results, logging nothing: the
// cafile it names (or the system bundle, located already) was validated
// before. Speculative requests (prefetch) are only prepared then.
func caChecked(ssl *php.Array) bool {
	caBundle.mu.Lock()
	defer caBundle.mu.Unlock()

	if cafile, ok := optionString(ssl, "cafile"); ok {
		_, known := caBundle.validity[cafile]

		return known
	}
	if _, ok := path(ssl, "capath"); ok {
		return true
	}
	if !caBundle.found {
		return false
	}
	_, known := caBundle.validity[caBundle.caPath]

	return known || isDir(caBundle.caPath)
}

// ResetCaBundle is CaBundle::reset().
func ResetCaBundle() {
	caBundle.mu.Lock()
	caBundle.caPath, caBundle.found, caBundle.validity = "", false, nil
	caBundle.mu.Unlock()
}

func caFileUsable(certFile string, logger Logger) bool {
	return caIsFile(certFile, logger) && caIsReadable(certFile, logger) && validateCaFile(certFile, logger)
}

func caDirUsable(certDir string, logger Logger) bool {
	return caIsDir(certDir, logger) && caIsReadable(certDir, logger) && caGlob(certDir+"/*", logger)
}

func caIsFile(certFile string, logger Logger) bool {
	fi, err := os.Stat(certFile)
	isFile := err == nil && fi.Mode().IsRegular()

	if !isFile && logger != nil {
		logger.Debug("Checked CA file "+certFile+" does not exist or it is not a file.", nil)
	}

	return isFile
}

func caIsDir(certDir string, logger Logger) bool {
	fi, err := os.Stat(certDir)
	isDir := err == nil && fi.IsDir()

	if !isDir && logger != nil {
		logger.Debug("Checked directory "+certDir+" does not exist or it is not a directory.", nil)
	}

	return isDir
}

func caIsReadable(path string, logger Logger) bool {
	isReadable := util.IsReadable(path)

	if !isReadable && logger != nil {
		logger.Debug("Checked file or directory "+path+" is not readable.", nil)
	}

	return isReadable
}

func caGlob(pattern string, logger Logger) bool {
	certs, err := filepath.Glob(pattern)
	if err != nil {
		if logger != nil {
			logger.Debug("An error occurred while trying to find certificates for pattern: "+pattern, nil)
		}

		return false
	}

	if len(certs) == 0 {
		if logger != nil {
			logger.Debug("No CA files found for pattern: "+pattern, nil)
		}

		return false
	}

	return true
}
