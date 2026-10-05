// Ports src/Composer/Util/Http/ProxyItem.php,
// src/Composer/Util/Http/RequestProxy.php and
// src/Composer/Util/Http/ProxyManager.php.

package http

import (
	"encoding/base64"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// ProxyItem is a proxy URL read from an environment variable.
type ProxyItem struct {
	url          string
	safeURL      string
	curlAuth     string
	optionsProxy string
	optionsAuth  string
}

// NewProxyItem is new ProxyItem($proxyUrl, $envName); errors are
// RuntimeExceptions.
func NewProxyItem(proxyURL, envName string) (*ProxyItem, error) {
	syntaxError := func(line int) error {
		return &util.RuntimeError{Message: "unsupported `" + envName + "` syntax", Site: phperr.At("ProxyItem.php", line)}
	}

	if strings.ContainsAny(proxyURL, "\r\n\t") {
		return nil, syntaxError(42)
	}

	proxy, ok := util.ParseURL(proxyURL)
	if !ok {
		return nil, syntaxError(45)
	}

	if !proxy.HasHost {
		return nil, &util.RuntimeError{Message: "unable to find proxy host in " + envName, Site: phperr.At("ProxyItem.php", 48)}
	}

	scheme := "http://"
	if proxy.HasScheme {
		scheme = php.Strtolower(proxy.Scheme) + "://"
	}

	item := &ProxyItem{}
	safe := ""

	if proxy.HasUser {
		safe = "***"
		user := proxy.User
		auth := php.Rawurldecode(proxy.User)

		if proxy.HasPass {
			safe += ":***"
			user += ":" + proxy.Pass
			auth += ":" + php.Rawurldecode(proxy.Pass)
		}

		safe += "@"

		if user != "" {
			item.curlAuth = user
			item.optionsAuth = "Proxy-Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(auth))
		}
	}

	port := -1

	switch {
	case proxy.HasPort:
		port = proxy.Port
	case scheme == "http://":
		port = 80
	case scheme == "https://":
		port = 443
	}

	// A port is needed because curl uses 1080 for http. Port 0 is reserved,
	// but is considered valid depending on the PHP or curl version.
	if port < 0 {
		return nil, &util.RuntimeError{Message: "unable to find proxy port in " + envName, Site: phperr.At("ProxyItem.php", 87)}
	}

	if port == 0 {
		return nil, &util.RuntimeError{Message: "port 0 is reserved in " + envName, Site: phperr.At("ProxyItem.php", 90)}
	}

	hostPort := proxy.Host + ":" + strconv.Itoa(port)
	item.url = scheme + hostPort
	item.safeURL = scheme + safe + hostPort

	switch scheme {
	case "http://":
		item.optionsProxy = "tcp://" + hostPort
	case "https://":
		item.optionsProxy = "ssl://" + hostPort
	default:
		item.optionsProxy = scheme + hostPort
	}

	return item, nil
}

// ToRequestProxy is toRequestProxy($scheme).
func (p *ProxyItem) ToRequestProxy(scheme string) *RequestProxy {
	httpOptions := php.ArrayOf("proxy", p.optionsProxy)
	if p.optionsAuth != "" {
		httpOptions.Set("header", p.optionsAuth)
	}

	if scheme == "http" {
		httpOptions.Set("request_fulluri", true)
	}

	return &RequestProxy{
		url:            p.url,
		auth:           p.curlAuth,
		contextOptions: php.ArrayOf("http", httpOptions),
		status:         p.safeURL,
	}
}

// RequestProxy is the proxy configuration of one request. Empty strings
// stand for PHP's nulls.
type RequestProxy struct {
	contextOptions *php.Array
	status         string
	url            string
	auth           string
}

// NewRequestProxy is new RequestProxy($url, $auth, $contextOptions,
// $status); "" and nil are null.
func NewRequestProxy(url, auth string, contextOptions *php.Array, status string) *RequestProxy {
	return &RequestProxy{url: url, auth: auth, contextOptions: contextOptions, status: status}
}

// NoneProxy is RequestProxy::none().
func NoneProxy() *RequestProxy { return &RequestProxy{} }

// NoProxy is RequestProxy::noProxy(): a request excluded by no_proxy.
func NoProxy() *RequestProxy { return &RequestProxy{status: "excluded by no_proxy"} }

// ContextOptions is getContextOptions(); nil for null.
func (p *RequestProxy) ContextOptions() *php.Array { return p.contextOptions }

// URL is the proxy URL, "" for none.
func (p *RequestProxy) URL() string { return p.url }

// CurlOptions is what getCurlOptions() hands curl, for the Go transport.
type CurlOptions struct {
	// Proxy is CURLOPT_PROXY, always set: "" tells curl to ignore the
	// proxy environment variables.
	Proxy string
	// NoProxy is whether CURLOPT_NOPROXY is set to "" (a proxy is used).
	NoProxy bool
	// UserPwd is CURLOPT_PROXYUSERPWD with CURLAUTH_BASIC, "" when unset.
	UserPwd string
	// CAInfo and CAPath are CURLOPT_PROXY_CAINFO and _CAPATH.
	CAInfo, CAPath string
}

// CurlOptions is getCurlOptions($sslOptions). Go supports HTTPS proxies,
// so the "Cannot use an HTTPS proxy" TransportException never happens.
func (p *RequestProxy) CurlOptions(sslOptions *php.Array) CurlOptions {
	opts := CurlOptions{Proxy: p.url, NoProxy: p.url != "", UserPwd: p.auth}

	if p.IsSecure() && sslOptions != nil {
		opts.CAInfo, _ = optionString(sslOptions, "cafile")
		opts.CAPath, _ = optionString(sslOptions, "capath")
	}

	return opts
}

// Status is getStatus(): the safe proxy URL or the no_proxy note, "" when
// there is none.
func (p *RequestProxy) Status() string { return p.status }

// StatusFormat is getStatus($format): the status put into format's %s, ""
// when there is none.
func (p *RequestProxy) StatusFormat(format string) (string, error) {
	if p.status == "" {
		return "", nil
	}

	if !strings.Contains(format, "%s") {
		return "", &util.InvalidArgumentError{Message: "String format specifier is missing", Site: phperr.At("RequestProxy.php", 128)}
	}

	return strings.Replace(format, "%s", p.status, 1), nil
}

// IsExcludedByNoProxy is isExcludedByNoProxy().
func (p *RequestProxy) IsExcludedByNoProxy() bool { return p.status != "" && p.url == "" }

// IsSecure is isSecure(): the proxy is reached over https.
func (p *RequestProxy) IsSecure() bool { return strings.HasPrefix(p.url, "https://") }

// ProxyManager reads the proxy environment variables once and gives each
// request its proxy.
type ProxyManager struct {
	err            string
	httpProxy      *ProxyItem
	httpsProxy     *ProxyItem
	noProxyHandler *util.NoProxyPattern
}

var (
	proxyManagerMu sync.Mutex
	proxyManager   *ProxyManager
)

// GetProxyManager is ProxyManager::getInstance(): the process-wide manager,
// built from the environment on first use (like Composer's singleton).
func GetProxyManager() *ProxyManager {
	proxyManagerMu.Lock()
	defer proxyManagerMu.Unlock()

	if proxyManager == nil {
		proxyManager = NewProxyManager(util.GetEnv)
	}

	return proxyManager
}

// ResetProxyManager is ProxyManager::reset().
func ResetProxyManager() {
	proxyManagerMu.Lock()
	proxyManager = nil
	proxyManagerMu.Unlock()
}

// NewProxyManager builds a manager from the variables getenv returns.
func NewProxyManager(getenv func(string) (string, bool)) *ProxyManager {
	m := &ProxyManager{}
	if err := m.proxyData(getenv); err != nil {
		m.err = err.Error()
	}

	return m
}

// HasProxy is hasProxy().
func (m *ProxyManager) HasProxy() bool { return m.httpProxy != nil || m.httpsProxy != nil }

// ProxyForRequest is getProxyForRequest($requestUrl).
func (m *ProxyManager) ProxyForRequest(requestURL string) (*RequestProxy, error) {
	if m.err != "" {
		return nil, transportError(phperr.At("ProxyManager.php", 75), "Unable to use a proxy: "+m.err, 400)
	}

	scheme := util.URLScheme(requestURL)

	var proxy *ProxyItem

	switch php.Strtolower(scheme) {
	case "http":
		proxy = m.httpProxy
	case "https":
		proxy = m.httpsProxy
	}

	if proxy == nil {
		return NoneProxy(), nil
	}

	if m.noProxyHandler != nil && m.noProxyHandler.Test(requestURL) {
		return NoProxy(), nil
	}

	return proxy.ToRequestProxy(scheme), nil
}

func (m *ProxyManager) proxyData(getenv func(string) (string, bool)) error {
	var err error

	// http_proxy/HTTP_PROXY are honoured because maestro is a CLI (PHP
	// ignores them elsewhere for security reasons).
	if env, name := proxyEnv(getenv, "http_proxy"); env != "" {
		if m.httpProxy, err = NewProxyItem(env, name); err != nil {
			return err
		}
	}

	if m.httpProxy == nil {
		if env, name := proxyEnv(getenv, "cgi_http_proxy"); env != "" {
			if m.httpProxy, err = NewProxyItem(env, name); err != nil {
				return err
			}
		}
	}

	if env, name := proxyEnv(getenv, "https_proxy"); env != "" {
		if m.httpsProxy, err = NewProxyItem(env, name); err != nil {
			return err
		}
	}

	if env, _ := proxyEnv(getenv, "no_proxy"); env != "" {
		m.noProxyHandler = util.NewNoProxyPattern(env)
	}

	return nil
}

// proxyEnv is getProxyEnv: the lowercase variable wins over the uppercase
// one; empty values are ignored.
func proxyEnv(getenv func(string) (string, bool), envName string) (value, name string) {
	for _, name := range [2]string{strings.ToLower(envName), strings.ToUpper(envName)} {
		if v, ok := getenv(name); ok && v != "" {
			return v, name
		}
	}

	return "", ""
}
