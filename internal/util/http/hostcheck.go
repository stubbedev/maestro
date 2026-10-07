// The peer name check of a verified certificate, as curl and PHP's
// stream wrapper do it rather than as crypto/tls does: both fall back on
// the subject's common name, which Go no longer reads.

package http

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"net"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// peerNameError is a certificate that does not name the peer: msg is
// curl's error (60), warning PHP's stream wrapper's.
type peerNameError struct {
	msg, warning string
}

func (e *peerNameError) Error() string {
	if e.msg != "" {
		return e.msg
	}

	return e.warning
}

// checkPeerName checks that a verified leaf names host, as curl does or,
// for stream, as PHP's stream wrapper does.
func checkPeerName(leaf *x509.Certificate, host string, stream bool) error {
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")

	if stream {
		return phpCheckPeerName(leaf, host)
	}

	return curlCheckPeerName(leaf, host)
}

// curlCheckPeerName is lib/vtls/openssl.c's ossl_verifyhost (libcurl
// 8.22): the subjectAltName entries of the target's type (DNS names for a
// host name, IP addresses for an address) are matched; when the
// certificate has no DNS or IP entry at all, the last common name of the
// subject is matched instead.
func curlCheckPeerName(leaf *x509.Certificate, host string) error {
	ip := net.ParseIP(host)

	for _, name := range leaf.DNSNames {
		if ip == nil && curlHostMatch(name, host) {
			return nil
		}
	}

	for _, addr := range leaf.IPAddresses {
		if ip != nil && addr.Equal(ip) {
			return nil
		}
	}

	if len(leaf.DNSNames) > 0 || len(leaf.IPAddresses) > 0 {
		target := "hostname"

		switch {
		case ip != nil && ip.To4() != nil:
			target = "IPv4 address"
		case ip != nil:
			target = "IPv6 address"
		}

		return &peerNameError{msg: "SSL: no alternative certificate subject name matches target " + target + " '" + host + "'"}
	}

	cn, ok := commonName(leaf.Subject, true)

	switch {
	case !ok:
		return &peerNameError{msg: "SSL: unable to obtain common name from peer certificate"}
	case !curlHostMatch(cn, host):
		return &peerNameError{msg: "SSL: certificate subject name '" + cn + "' does not match target hostname '" + host + "'"}
	}

	return nil
}

// curlHostMatch is lib/vtls/hostcheck.c's Curl_cert_hostcheck: names
// compare case-insensitively without a trailing dot; a pattern "*.rest"
// with at least two dots matches a host name (not an address) whose first
// label is followed by rest.
func curlHostMatch(pattern, host string) bool {
	pattern = strings.TrimSuffix(pattern, ".")
	host = strings.TrimSuffix(host, ".")

	switch {
	case pattern == "" || host == "":
		return false
	case !strings.HasPrefix(pattern, "*."):
		return php.Strcasecmp(pattern, host) == 0
	case net.ParseIP(host) != nil:
		return false
	case strings.LastIndexByte(pattern, '.') == 1:
		// a single dot: too wide a wildcard, compared as is
		return php.Strcasecmp(pattern, host) == 0
	}

	i := strings.IndexByte(host, '.')

	return i >= 0 && php.Strcasecmp(pattern[1:], host[i:]) == 0
}

// phpCheckPeerName is ext/openssl/xp_ssl.c's peer name check (PHP 8.4,
// php_openssl_apply_peer_verification): a DNS subjectAltName matching the
// name (php_openssl_matches_wildcard_name), an IP one equal to it, else
// the subject's first common name matching it, with the warning
// php_openssl_matches_common_name raises when it does not.
func phpCheckPeerName(leaf *x509.Certificate, host string) error {
	for _, name := range leaf.DNSNames {
		if phpWildcardMatch(host, strings.TrimSuffix(name, ".")) {
			return nil
		}
	}

	for _, addr := range leaf.IPAddresses {
		if ip := net.ParseIP(host); ip != nil && addr.Equal(ip) {
			return nil
		}
	}

	cn, ok := commonName(leaf.Subject, false)

	switch {
	case !ok:
		return &peerNameError{warning: "Unable to locate peer certificate CN"}
	case strings.IndexByte(cn, 0) >= 0:
		return &peerNameError{warning: "Peer certificate CN=`" + cn + "' is malformed"}
	case !phpWildcardMatch(host, cn):
		return &peerNameError{warning: "Peer certificate CN=`" + cn + "' did not match expected CN=`" + host + "'"}
	}

	return nil
}

// phpWildcardMatch is php_openssl_matches_wildcard_name: equal ignoring
// case, or a '*' in the first label standing for any run of characters
// without a dot.
func phpWildcardMatch(subject, certName string) bool {
	if php.Strcasecmp(subject, certName) == 0 {
		return true
	}

	wildcard := strings.IndexByte(certName, '*')
	if wildcard < 0 || strings.IndexByte(certName[:wildcard], '.') >= 0 {
		return false
	}

	prefix, suffix := certName[:wildcard], certName[wildcard+1:]
	if len(prefix)+len(suffix) > len(subject) || php.Strcasecmp(subject[:len(prefix)], prefix) != 0 {
		return false
	}

	end := len(subject) - len(suffix)

	return php.Strcasecmp(subject[end:], suffix) == 0 && strings.IndexByte(subject[len(prefix):end], '.') < 0
}

var oidCommonName = asn1.ObjectIdentifier{2, 5, 4, 3}

// commonName is the subject's last common name (curl looks for the last
// one, "the most significant") or first (PHP's
// X509_NAME_get_text_by_NID), in the order of the encoded name.
func commonName(subject pkix.Name, last bool) (string, bool) {
	var (
		cn    string
		found bool
	)

	for _, atv := range subject.Names {
		if !atv.Type.Equal(oidCommonName) {
			continue
		}

		s, ok := atv.Value.(string)
		if !ok {
			continue
		}

		cn, found = s, true

		if !last {
			break
		}
	}

	return cn, found
}
