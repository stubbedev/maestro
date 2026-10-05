// Ports src/Composer/Util/StreamContextFactory.php. A stream context is
// represented by its options array, which RemoteFilesystem's transport
// reads.

package http

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// GetContext is StreamContextFactory::getContext($url, $defaultOptions):
// the options of the stream context a RemoteFilesystem request uses.
func GetContext(url string, defaultOptions *php.Array, rt Runtime) (*php.Array, error) {
	options := php.ArrayOf("http", php.ArrayOf(
		// specify defaults again to try and work better with curlwrappers enabled
		"follow_location", 1,
		"max_redirects", 20,
	))

	initialized, err := InitOptions(url, defaultOptions, false, rt)
	if err != nil {
		return nil, err
	}

	options = php.ArrayReplaceRecursive(options, initialized)

	defaults := cloneOptions(defaultOptions)
	if h, ok := defaults.Get("http"); ok {
		if a, ok := h.(*php.Array); ok {
			a.Delete("header")
		}
	}

	options = php.ArrayReplaceRecursive(options, defaults).Clone()

	if v, ok := path(options, "http", "header"); ok {
		HTTPOptions(options).Set("header", fixHTTPHeaderField(v))
	}

	return options, nil
}

// InitOptions is StreamContextFactory::initOptions($url, $options,
// $forCurl): the header list normalised, the proxy options added (for
// streams) and the User-Agent header added when missing. options is not
// modified.
func InitOptions(url string, options *php.Array, forCurl bool, rt Runtime) (*php.Array, error) {
	return initOptions(url, cloneOptions(options), forCurl, rt)
}

// initOptions is InitOptions modifying options, which the caller owns.
func initOptions(url string, options *php.Array, forCurl bool, rt Runtime) (*php.Array, error) {
	httpOptions := HTTPOptions(options)

	// Make sure the headers are in an array form.
	switch h, _ := httpOptions.Get("header"); h := h.(type) {
	case nil:
		httpOptions.Set("header", php.NewArray())
	case string:
		httpOptions.Set("header", php.StringList(splitCRLF(h)))
	}

	// Add stream proxy options if there is a proxy.
	if !forCurl {
		proxy, err := GetProxyManager().ProxyForRequest(url)
		if err != nil {
			return nil, err
		}

		if proxyOptions := proxy.ContextOptions(); proxyOptions != nil {
			isHTTPSRequest := strings.HasPrefix(url, "https://")
			if proxy.IsSecure() && isHTTPSRequest {
				return nil, transportError(phperr.At("StreamContextFactory.php", 89), "You must enable the curl extension to make https requests through a secure proxy.", 400)
			}

			proxyOptions = proxyOptions.Clone()

			// Header will be a Proxy-Authorization string or not set.
			if header, ok := path(proxyOptions, "http", "header"); ok {
				appendHeader(options, php.ToString(header))
				HTTPOptions(proxyOptions).Delete("header")
			}

			// both sides are private copies, so the result may share them
			options = php.ArrayReplaceRecursive(options, proxyOptions)
		}
	}

	if !headerContains(options, "user-agent") {
		appendHeader(options, UserAgent(forCurl, rt))
	}

	return options, nil
}

// Infallible: the pattern does bounded work per start position, so Preg
// cannot throw on it and its call sites ignore the error.
var runningCommandStrip = php.MustCompile(`{[^a-z0-9:_-]}i`)

// UserAgent is the User-Agent header initOptions adds:
//
//	User-Agent: Composer/2.10.3 (Linux; 6.8.0; PHP 8.3.6; maestro 1.0.0; Platform-PHP 8.1.0; CI; cmd:install)
//
// maestro reports itself as the Composer release it ports, so servers that
// key behaviour off the Composer version (Packagist's warning-versions,
// mirrors) treat it like that Composer. The slot where Composer names its
// HTTP client ("cURL 8.5.0" or "streams") holds "maestro" and maestro's
// version instead, and the PHP version is the php binary's ("PHP unknown"
// when there is none).
func UserAgent(_ bool, rt Runtime) string {
	if rt == nil {
		rt = defaultRuntime
	}

	sysname, release := uname()

	phpVersion := rt.PHPVersion()
	if phpVersion == "" {
		phpVersion = "unknown"
	}

	httpVersion := "maestro"
	if v := rt.ClientVersion(); v != "" {
		httpVersion += " " + v
	}

	var b strings.Builder

	b.WriteString("User-Agent: Composer/")
	b.WriteString(ComposerVersion)
	b.WriteString(" (")
	b.WriteString(sysname)
	b.WriteString("; ")
	b.WriteString(release)
	b.WriteString("; PHP ")
	b.WriteString(phpVersion)
	b.WriteString("; ")
	b.WriteString(httpVersion)

	if v := rt.PlatformPHPVersion(); v != "" {
		b.WriteString("; Platform-PHP ")
		b.WriteString(v)
	}

	if ci, ok := util.GetEnv("CI"); ok && php.ToBool(ci) {
		b.WriteString("; CI")
	}

	command, hasCommand := rt.RunningCommand()
	if hasCommand {
		command, _, _ = runningCommandStrip.Replace(command, "", -1)
	}

	if operation, ok := rt.RunningOperation(); ok && (!hasCommand || operation != command) {
		if hasCommand {
			command += "," + operation
		} else {
			command, hasCommand = operation, true
		}
	}

	if hasCommand {
		b.WriteString("; cmd:")
		b.WriteString(command)
	}

	b.WriteByte(')')

	return b.String()
}

// TLS cipher list Composer configures for PHP streams.
const tlsCiphers = "ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-GCM-SHA384:" +
	"ECDHE-ECDSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:DHE-DSS-AES128-GCM-SHA256:kEDH+AESGCM:" +
	"ECDHE-RSA-AES128-SHA256:ECDHE-ECDSA-AES128-SHA256:ECDHE-RSA-AES128-SHA:ECDHE-ECDSA-AES128-SHA:" +
	"ECDHE-RSA-AES256-SHA384:ECDHE-ECDSA-AES256-SHA384:ECDHE-RSA-AES256-SHA:ECDHE-ECDSA-AES256-SHA:" +
	"DHE-RSA-AES128-SHA256:DHE-RSA-AES128-SHA:DHE-DSS-AES128-SHA256:DHE-RSA-AES256-SHA256:DHE-DSS-AES256-SHA:" +
	"DHE-RSA-AES256-SHA:AES128-GCM-SHA256:AES256-GCM-SHA384:AES128-SHA256:AES256-SHA256:AES128-SHA:AES256-SHA:" +
	"AES:CAMELLIA:!aNULL:!eNULL:!EXPORT:!DES:!3DES:!RC4:!MD5:!PSK:!aECDH:!EDH-DSS-DES-CBC3-SHA:" +
	"!EDH-RSA-DES-CBC3-SHA:!KRB5-DES-CBC3-SHA"

// GetTLSDefaults is StreamContextFactory::getTlsDefaults($options,
// $logger): the ssl options every request starts from, with the CA bundle
// located and validated. logger may be nil.
func GetTLSDefaults(options *php.Array, logger Logger) (*php.Array, error) {
	ssl := php.ArrayOf(
		"ciphers", tlsCiphers,
		"verify_peer", true,
		"verify_depth", 7,
		"SNI_enabled", true,
		"capture_peer_cert", true,
	)

	if v, ok := path(options, "ssl"); ok {
		if a, ok := v.(*php.Array); ok {
			ssl = php.ArrayReplaceRecursive(ssl, a).Clone()
		}
	}

	_, hasCafile := path(ssl, "cafile")
	_, hasCapath := path(ssl, "capath")

	if !hasCafile && !hasCapath {
		result, err := SystemCaRootBundlePath(logger)
		if err != nil {
			return nil, err
		}

		if isDir(result) {
			ssl.Set("capath", result)
			hasCapath = true
		} else {
			ssl.Set("cafile", result)
			hasCafile = true
		}
	}

	if hasCafile {
		cafile, _ := optionString(ssl, "cafile")
		if !util.IsReadable(cafile) || !ValidateCaFile(cafile, logger) {
			return nil, transportError(phperr.At("StreamContextFactory.php", 229), "The configured cafile was not valid or could not be read.", 400)
		}
	}

	if hasCapath {
		capath, _ := optionString(ssl, "capath")
		if !isDir(capath) || !util.IsReadable(capath) {
			return nil, transportError(phperr.At("StreamContextFactory.php", 233), "The configured capath was not valid or could not be read.", 400)
		}
	}

	ssl.Set("disable_compression", true)

	return php.ArrayOf("ssl", ssl), nil
}

// fixHTTPHeaderField is fixHttpHeaderField: the header lines as an array
// with the Content-Type lines moved to the end. Composer does it with an
// inconsistent uasort comparator; for header lists PHP's insertion sort
// makes that a stable partition, keys preserved.
func fixHTTPHeaderField(header any) *php.Array {
	var lines *php.Array

	switch h := header.(type) {
	case *php.Array:
		lines = h
	default:
		lines = php.StringList(splitCRLF(php.ToString(header)))
	}

	out := php.NewArrayCap(lines.Len())

	var contentType []php.Key

	for k, v := range lines.All() {
		if php.Strncasecmp(php.ToString(v), "content-type", 12) == 0 {
			contentType = append(contentType, k)

			continue
		}

		out.SetKey(k, v)
	}

	for _, k := range contentType {
		v, _ := lines.GetKey(k)
		out.SetKey(k, v)
	}

	return out
}

// appendHeader is $options['http']['header'][] = $line.
func appendHeader(options *php.Array, line string) { AppendHeader(HTTPOptions(options), line) }

// AppendHeader performs
//
//	if (isset($http['header'])) { $http['header'] = (array) $http['header']; }
//	$http['header'][] = $header;
//
// on the $options['http'] array (FilterListApiClient, ComposerRepository
// and the stream context code).
func AppendHeader(httpOptions *php.Array, header string) {
	headers, _ := httpOptions.Get("header")
	var list *php.Array
	switch h := headers.(type) {
	case *php.Array:
		list = h
	case *php.Object:
		list = h.ToArray()
		httpOptions.Set("header", list)
	default:
		list = php.NewArray()
		if headers != nil {
			list.Append(headers)
		}
		httpOptions.Set("header", list)
	}

	list.Append(header)
}

// headerContains is stripos(implode(”, $options['http']['header']),
// $needle) !== false.
func headerContains(options *php.Array, needle string) bool {
	return php.Stripos(strings.Join(headerList(options), ""), needle) >= 0
}

// isDir is is_dir().
func isDir(path string) bool {
	fi, err := os.Stat(path)

	return err == nil && fi.IsDir()
}
