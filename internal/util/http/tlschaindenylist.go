// Ports src/Composer/Util/Http/TlsChainDenylist.php.
//
// CurlDownloader inspects the chain curl reports (CURLINFO_CERTINFO) and
// RemoteFilesystem the one PHP's stream context captures after the
// request, both keyed by host:port because that is what curl reuses
// connections by. Go's transport verifies the chain of every handshake
// before the request is written (transport.go's checkDistrustedChain), so
// only the verdict CurlDownloader caches for a host, and the exception the
// connection fails with, are needed.

package http

import (
	"strconv"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// TlsChainHostKey is TlsChainDenylist::getHostKey($url): host:port for
// https URLs, as that is what a connection is reused by; false for the
// others.
func TlsChainHostKey(url string) (string, bool) {
	if !hasHTTPSPrefix(url) {
		return "", false
	}

	parts, ok := util.ParseURL(url)
	if !ok || !parts.HasHost {
		return "", false
	}

	port := 443
	if parts.HasPort {
		port = parts.Port
	}

	return php.Strtolower(parts.Host) + ":" + strconv.Itoa(port), true
}

// hasHTTPSPrefix is 0 === stripos($url, 'https://').
func hasHTTPSPrefix(url string) bool {
	return len(url) >= 8 && php.Strcasecmp(url[:8], "https://") == 0
}

// NewTlsChainError is TlsChainDenylist::createException($url, $certName,
// $requestSent) as Go's transport raises it: the chain is checked once the
// handshake is done and before the request and its credentials are sent,
// so requestSent is always false there.
//
// It is not a TransportError so that callers falling back on transport
// errors do not swallow it.
func NewTlsChainError(url, certName string) error {
	return &util.SecurityError{Message: `The TLS certificate chain presented for "` + util.SanitizeURL(url) + `" contains the distrusted certificate "` + certName + `", this may be a man-in-the-middle attack. The request was aborted before it was sent.`}
}
