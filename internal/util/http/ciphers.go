// The ssl "ciphers" option of PHP's stream wrapper: an OpenSSL cipher
// list (SSL_CTX_set_cipher_list), evaluated as OpenSSL 3 evaluates it
// (ssl/ssl_ciph.c) to choose the TLS 1.2 suites crypto/tls offers. TLS 1.3
// suites are not affected by it, in OpenSSL as in crypto/tls. Only the set
// of suites matters: crypto/tls ignores the order of Config.CipherSuites.

package http

import (
	"crypto/tls"
	"strings"
	"sync"
)

// opensslCiphers is OpenSSL 3's cipher table below TLS 1.3 (`openssl
// ciphers -v ALL:COMPLEMENTOFALL`): name, minimum protocol, key exchange,
// authentication, encryption, MAC, and "nd" for the ciphers outside
// DEFAULT (COMPLEMENTOFDEFAULT).
const opensslCiphers = `
ECDHE-ECDSA-AES256-GCM-SHA384 TLSv1.2 ECDH ECDSA AESGCM(256) AEAD
ECDHE-RSA-AES256-GCM-SHA384 TLSv1.2 ECDH RSA AESGCM(256) AEAD
DHE-DSS-AES256-GCM-SHA384 TLSv1.2 DH DSS AESGCM(256) AEAD nd
DHE-RSA-AES256-GCM-SHA384 TLSv1.2 DH RSA AESGCM(256) AEAD
ECDHE-ECDSA-CHACHA20-POLY1305 TLSv1.2 ECDH ECDSA CHACHA20/POLY1305(256) AEAD
ECDHE-RSA-CHACHA20-POLY1305 TLSv1.2 ECDH RSA CHACHA20/POLY1305(256) AEAD
DHE-RSA-CHACHA20-POLY1305 TLSv1.2 DH RSA CHACHA20/POLY1305(256) AEAD
ECDHE-ECDSA-AES256-CCM TLSv1.2 ECDH ECDSA AESCCM(256) AEAD nd
DHE-RSA-AES256-CCM TLSv1.2 DH RSA AESCCM(256) AEAD nd
ECDHE-ECDSA-ARIA256-GCM-SHA384 TLSv1.2 ECDH ECDSA ARIAGCM(256) AEAD nd
ECDHE-ARIA256-GCM-SHA384 TLSv1.2 ECDH RSA ARIAGCM(256) AEAD nd
DHE-DSS-ARIA256-GCM-SHA384 TLSv1.2 DH DSS ARIAGCM(256) AEAD nd
DHE-RSA-ARIA256-GCM-SHA384 TLSv1.2 DH RSA ARIAGCM(256) AEAD nd
ADH-AES256-GCM-SHA384 TLSv1.2 DH None AESGCM(256) AEAD nd
ECDHE-ECDSA-AES128-GCM-SHA256 TLSv1.2 ECDH ECDSA AESGCM(128) AEAD
ECDHE-RSA-AES128-GCM-SHA256 TLSv1.2 ECDH RSA AESGCM(128) AEAD
DHE-DSS-AES128-GCM-SHA256 TLSv1.2 DH DSS AESGCM(128) AEAD nd
DHE-RSA-AES128-GCM-SHA256 TLSv1.2 DH RSA AESGCM(128) AEAD
ECDHE-ECDSA-AES128-CCM TLSv1.2 ECDH ECDSA AESCCM(128) AEAD nd
DHE-RSA-AES128-CCM TLSv1.2 DH RSA AESCCM(128) AEAD nd
ECDHE-ECDSA-ARIA128-GCM-SHA256 TLSv1.2 ECDH ECDSA ARIAGCM(128) AEAD nd
ECDHE-ARIA128-GCM-SHA256 TLSv1.2 ECDH RSA ARIAGCM(128) AEAD nd
DHE-DSS-ARIA128-GCM-SHA256 TLSv1.2 DH DSS ARIAGCM(128) AEAD nd
DHE-RSA-ARIA128-GCM-SHA256 TLSv1.2 DH RSA ARIAGCM(128) AEAD nd
ADH-AES128-GCM-SHA256 TLSv1.2 DH None AESGCM(128) AEAD nd
ECDHE-ECDSA-AES256-CCM8 TLSv1.2 ECDH ECDSA AESCCM8(256) AEAD nd
ECDHE-ECDSA-AES128-CCM8 TLSv1.2 ECDH ECDSA AESCCM8(128) AEAD nd
DHE-RSA-AES256-CCM8 TLSv1.2 DH RSA AESCCM8(256) AEAD nd
DHE-RSA-AES128-CCM8 TLSv1.2 DH RSA AESCCM8(128) AEAD nd
ECDHE-ECDSA-AES256-SHA384 TLSv1.2 ECDH ECDSA AES(256) SHA384
ECDHE-RSA-AES256-SHA384 TLSv1.2 ECDH RSA AES(256) SHA384
DHE-RSA-AES256-SHA256 TLSv1.2 DH RSA AES(256) SHA256
DHE-DSS-AES256-SHA256 TLSv1.2 DH DSS AES(256) SHA256 nd
ECDHE-ECDSA-CAMELLIA256-SHA384 TLSv1.2 ECDH ECDSA Camellia(256) SHA384 nd
ECDHE-RSA-CAMELLIA256-SHA384 TLSv1.2 ECDH RSA Camellia(256) SHA384 nd
DHE-RSA-CAMELLIA256-SHA256 TLSv1.2 DH RSA Camellia(256) SHA256 nd
DHE-DSS-CAMELLIA256-SHA256 TLSv1.2 DH DSS Camellia(256) SHA256 nd
ADH-AES256-SHA256 TLSv1.2 DH None AES(256) SHA256 nd
ADH-CAMELLIA256-SHA256 TLSv1.2 DH None Camellia(256) SHA256 nd
ECDHE-ECDSA-AES128-SHA256 TLSv1.2 ECDH ECDSA AES(128) SHA256
ECDHE-RSA-AES128-SHA256 TLSv1.2 ECDH RSA AES(128) SHA256
DHE-RSA-AES128-SHA256 TLSv1.2 DH RSA AES(128) SHA256
DHE-DSS-AES128-SHA256 TLSv1.2 DH DSS AES(128) SHA256 nd
ECDHE-ECDSA-CAMELLIA128-SHA256 TLSv1.2 ECDH ECDSA Camellia(128) SHA256 nd
ECDHE-RSA-CAMELLIA128-SHA256 TLSv1.2 ECDH RSA Camellia(128) SHA256 nd
DHE-RSA-CAMELLIA128-SHA256 TLSv1.2 DH RSA Camellia(128) SHA256 nd
DHE-DSS-CAMELLIA128-SHA256 TLSv1.2 DH DSS Camellia(128) SHA256 nd
ADH-AES128-SHA256 TLSv1.2 DH None AES(128) SHA256 nd
ADH-CAMELLIA128-SHA256 TLSv1.2 DH None Camellia(128) SHA256 nd
ECDHE-ECDSA-AES256-SHA TLSv1 ECDH ECDSA AES(256) SHA1
ECDHE-RSA-AES256-SHA TLSv1 ECDH RSA AES(256) SHA1
DHE-RSA-AES256-SHA SSLv3 DH RSA AES(256) SHA1
DHE-DSS-AES256-SHA SSLv3 DH DSS AES(256) SHA1 nd
DHE-RSA-CAMELLIA256-SHA SSLv3 DH RSA Camellia(256) SHA1 nd
DHE-DSS-CAMELLIA256-SHA SSLv3 DH DSS Camellia(256) SHA1 nd
AECDH-AES256-SHA TLSv1 ECDH None AES(256) SHA1 nd
ADH-AES256-SHA SSLv3 DH None AES(256) SHA1 nd
ADH-CAMELLIA256-SHA SSLv3 DH None Camellia(256) SHA1 nd
ECDHE-ECDSA-AES128-SHA TLSv1 ECDH ECDSA AES(128) SHA1
ECDHE-RSA-AES128-SHA TLSv1 ECDH RSA AES(128) SHA1
DHE-RSA-AES128-SHA SSLv3 DH RSA AES(128) SHA1
DHE-DSS-AES128-SHA SSLv3 DH DSS AES(128) SHA1 nd
DHE-RSA-CAMELLIA128-SHA SSLv3 DH RSA Camellia(128) SHA1 nd
DHE-DSS-CAMELLIA128-SHA SSLv3 DH DSS Camellia(128) SHA1 nd
AECDH-AES128-SHA TLSv1 ECDH None AES(128) SHA1 nd
ADH-AES128-SHA SSLv3 DH None AES(128) SHA1 nd
ADH-CAMELLIA128-SHA SSLv3 DH None Camellia(128) SHA1 nd
RSA-PSK-AES256-GCM-SHA384 TLSv1.2 RSAPSK RSA AESGCM(256) AEAD
DHE-PSK-AES256-GCM-SHA384 TLSv1.2 DHEPSK PSK AESGCM(256) AEAD
RSA-PSK-CHACHA20-POLY1305 TLSv1.2 RSAPSK RSA CHACHA20/POLY1305(256) AEAD
DHE-PSK-CHACHA20-POLY1305 TLSv1.2 DHEPSK PSK CHACHA20/POLY1305(256) AEAD
ECDHE-PSK-CHACHA20-POLY1305 TLSv1.2 ECDHEPSK PSK CHACHA20/POLY1305(256) AEAD
DHE-PSK-AES256-CCM TLSv1.2 DHEPSK PSK AESCCM(256) AEAD nd
RSA-PSK-ARIA256-GCM-SHA384 TLSv1.2 RSAPSK RSA ARIAGCM(256) AEAD nd
DHE-PSK-ARIA256-GCM-SHA384 TLSv1.2 DHEPSK PSK ARIAGCM(256) AEAD nd
AES256-GCM-SHA384 TLSv1.2 RSA RSA AESGCM(256) AEAD
AES256-CCM TLSv1.2 RSA RSA AESCCM(256) AEAD nd
ARIA256-GCM-SHA384 TLSv1.2 RSA RSA ARIAGCM(256) AEAD nd
PSK-AES256-GCM-SHA384 TLSv1.2 PSK PSK AESGCM(256) AEAD
PSK-CHACHA20-POLY1305 TLSv1.2 PSK PSK CHACHA20/POLY1305(256) AEAD
PSK-AES256-CCM TLSv1.2 PSK PSK AESCCM(256) AEAD nd
PSK-ARIA256-GCM-SHA384 TLSv1.2 PSK PSK ARIAGCM(256) AEAD nd
RSA-PSK-AES128-GCM-SHA256 TLSv1.2 RSAPSK RSA AESGCM(128) AEAD
DHE-PSK-AES128-GCM-SHA256 TLSv1.2 DHEPSK PSK AESGCM(128) AEAD
DHE-PSK-AES128-CCM TLSv1.2 DHEPSK PSK AESCCM(128) AEAD nd
RSA-PSK-ARIA128-GCM-SHA256 TLSv1.2 RSAPSK RSA ARIAGCM(128) AEAD nd
DHE-PSK-ARIA128-GCM-SHA256 TLSv1.2 DHEPSK PSK ARIAGCM(128) AEAD nd
AES128-GCM-SHA256 TLSv1.2 RSA RSA AESGCM(128) AEAD
AES128-CCM TLSv1.2 RSA RSA AESCCM(128) AEAD nd
ARIA128-GCM-SHA256 TLSv1.2 RSA RSA ARIAGCM(128) AEAD nd
PSK-AES128-GCM-SHA256 TLSv1.2 PSK PSK AESGCM(128) AEAD
PSK-AES128-CCM TLSv1.2 PSK PSK AESCCM(128) AEAD nd
PSK-ARIA128-GCM-SHA256 TLSv1.2 PSK PSK ARIAGCM(128) AEAD nd
DHE-PSK-AES256-CCM8 TLSv1.2 DHEPSK PSK AESCCM8(256) AEAD nd
DHE-PSK-AES128-CCM8 TLSv1.2 DHEPSK PSK AESCCM8(128) AEAD nd
AES256-CCM8 TLSv1.2 RSA RSA AESCCM8(256) AEAD nd
AES128-CCM8 TLSv1.2 RSA RSA AESCCM8(128) AEAD nd
PSK-AES256-CCM8 TLSv1.2 PSK PSK AESCCM8(256) AEAD nd
PSK-AES128-CCM8 TLSv1.2 PSK PSK AESCCM8(128) AEAD nd
AES256-SHA256 TLSv1.2 RSA RSA AES(256) SHA256
CAMELLIA256-SHA256 TLSv1.2 RSA RSA Camellia(256) SHA256 nd
AES128-SHA256 TLSv1.2 RSA RSA AES(128) SHA256
CAMELLIA128-SHA256 TLSv1.2 RSA RSA Camellia(128) SHA256 nd
ECDHE-PSK-AES256-CBC-SHA384 TLSv1 ECDHEPSK PSK AES(256) SHA384
ECDHE-PSK-AES256-CBC-SHA TLSv1 ECDHEPSK PSK AES(256) SHA1
SRP-DSS-AES-256-CBC-SHA SSLv3 SRP DSS AES(256) SHA1 nd
SRP-RSA-AES-256-CBC-SHA SSLv3 SRP RSA AES(256) SHA1
SRP-AES-256-CBC-SHA SSLv3 SRP SRP AES(256) SHA1
RSA-PSK-AES256-CBC-SHA384 TLSv1 RSAPSK RSA AES(256) SHA384
DHE-PSK-AES256-CBC-SHA384 TLSv1 DHEPSK PSK AES(256) SHA384
RSA-PSK-AES256-CBC-SHA SSLv3 RSAPSK RSA AES(256) SHA1
DHE-PSK-AES256-CBC-SHA SSLv3 DHEPSK PSK AES(256) SHA1
ECDHE-PSK-CAMELLIA256-SHA384 TLSv1 ECDHEPSK PSK Camellia(256) SHA384 nd
RSA-PSK-CAMELLIA256-SHA384 TLSv1 RSAPSK RSA Camellia(256) SHA384 nd
DHE-PSK-CAMELLIA256-SHA384 TLSv1 DHEPSK PSK Camellia(256) SHA384 nd
AES256-SHA SSLv3 RSA RSA AES(256) SHA1
CAMELLIA256-SHA SSLv3 RSA RSA Camellia(256) SHA1 nd
PSK-AES256-CBC-SHA384 TLSv1 PSK PSK AES(256) SHA384
PSK-AES256-CBC-SHA SSLv3 PSK PSK AES(256) SHA1
PSK-CAMELLIA256-SHA384 TLSv1 PSK PSK Camellia(256) SHA384 nd
ECDHE-PSK-AES128-CBC-SHA256 TLSv1 ECDHEPSK PSK AES(128) SHA256
ECDHE-PSK-AES128-CBC-SHA TLSv1 ECDHEPSK PSK AES(128) SHA1
SRP-DSS-AES-128-CBC-SHA SSLv3 SRP DSS AES(128) SHA1 nd
SRP-RSA-AES-128-CBC-SHA SSLv3 SRP RSA AES(128) SHA1
SRP-AES-128-CBC-SHA SSLv3 SRP SRP AES(128) SHA1
RSA-PSK-AES128-CBC-SHA256 TLSv1 RSAPSK RSA AES(128) SHA256
DHE-PSK-AES128-CBC-SHA256 TLSv1 DHEPSK PSK AES(128) SHA256
RSA-PSK-AES128-CBC-SHA SSLv3 RSAPSK RSA AES(128) SHA1
DHE-PSK-AES128-CBC-SHA SSLv3 DHEPSK PSK AES(128) SHA1
ECDHE-PSK-CAMELLIA128-SHA256 TLSv1 ECDHEPSK PSK Camellia(128) SHA256 nd
RSA-PSK-CAMELLIA128-SHA256 TLSv1 RSAPSK RSA Camellia(128) SHA256 nd
DHE-PSK-CAMELLIA128-SHA256 TLSv1 DHEPSK PSK Camellia(128) SHA256 nd
AES128-SHA SSLv3 RSA RSA AES(128) SHA1
CAMELLIA128-SHA SSLv3 RSA RSA Camellia(128) SHA1 nd
PSK-AES128-CBC-SHA256 TLSv1 PSK PSK AES(128) SHA256
PSK-AES128-CBC-SHA SSLv3 PSK PSK AES(128) SHA1
PSK-CAMELLIA128-SHA256 TLSv1 PSK PSK Camellia(128) SHA256 nd
ECDHE-ECDSA-NULL-SHA TLSv1 ECDH ECDSA None SHA1
ECDHE-RSA-NULL-SHA TLSv1 ECDH RSA None SHA1
AECDH-NULL-SHA TLSv1 ECDH None None SHA1
NULL-SHA256 TLSv1.2 RSA RSA None SHA256
ECDHE-PSK-NULL-SHA384 TLSv1 ECDHEPSK PSK None SHA384
ECDHE-PSK-NULL-SHA256 TLSv1 ECDHEPSK PSK None SHA256
ECDHE-PSK-NULL-SHA TLSv1 ECDHEPSK PSK None SHA1
RSA-PSK-NULL-SHA384 TLSv1 RSAPSK RSA None SHA384
RSA-PSK-NULL-SHA256 TLSv1 RSAPSK RSA None SHA256
DHE-PSK-NULL-SHA384 TLSv1 DHEPSK PSK None SHA384
DHE-PSK-NULL-SHA256 TLSv1 DHEPSK PSK None SHA256
RSA-PSK-NULL-SHA SSLv3 RSAPSK RSA None SHA1
DHE-PSK-NULL-SHA SSLv3 DHEPSK PSK None SHA1
NULL-SHA SSLv3 RSA RSA None SHA1
NULL-MD5 SSLv3 RSA RSA None MD5
PSK-NULL-SHA384 TLSv1 PSK PSK None SHA384
PSK-NULL-SHA256 TLSv1 PSK PSK None SHA256
PSK-NULL-SHA SSLv3 PSK PSK None SHA1
`

// opensslCipher is a row of opensslCiphers.
type opensslCipher struct {
	name, version, kx, au, enc, mac string
	notDefault                      bool
	// suite is crypto/tls's ID for it, 0 when crypto/tls lacks it.
	suite uint16
}

var cipherTable = sync.OnceValue(func() []opensslCipher {
	goSuites := map[string]uint16{}
	for _, s := range tls.CipherSuites() {
		goSuites[s.Name] = s.ID
	}

	for _, s := range tls.InsecureCipherSuites() {
		goSuites[s.Name] = s.ID
	}

	var table []opensslCipher

	for line := range strings.Lines(strings.TrimSpace(opensslCiphers)) {
		f := strings.Fields(line)
		c := opensslCipher{name: f[0], version: f[1], kx: f[2], au: f[3], enc: f[4], mac: f[5], notDefault: len(f) > 6}
		c.suite = goSuites[ianaName(c)]
		table = append(table, c)
	}

	return table
})

// ianaName is the RFC name crypto/tls knows a cipher by, for the kinds it
// implements (ECDHE or RSA key exchange; AES or ChaCha20).
func ianaName(c opensslCipher) string {
	var kx string

	switch {
	case c.kx == "ECDH" && c.au == "RSA":
		kx = "ECDHE_RSA"
	case c.kx == "ECDH" && c.au == "ECDSA":
		kx = "ECDHE_ECDSA"
	case c.kx == "RSA" && c.au == "RSA":
		kx = "RSA"
	default:
		return ""
	}

	switch c.enc {
	case "CHACHA20/POLY1305(256)":
		return "TLS_" + kx + "_WITH_CHACHA20_POLY1305_SHA256"
	case "AESGCM(128)":
		return "TLS_" + kx + "_WITH_AES_128_GCM_SHA256"
	case "AESGCM(256)":
		return "TLS_" + kx + "_WITH_AES_256_GCM_SHA384"
	case "AES(128)", "AES(256)":
		bits := c.enc[4:7]
		mac := map[string]string{"SHA1": "SHA", "SHA256": "SHA256", "SHA384": "SHA384"}[c.mac]

		return "TLS_" + kx + "_WITH_AES_" + bits + "_CBC_" + mac
	}

	return ""
}

// cipherAliases are OpenSSL's cipher_aliases that select from the table.
var cipherAliases = map[string]func(c opensslCipher) bool{
	"ALL":                 func(c opensslCipher) bool { return c.enc != "None" },
	"COMPLEMENTOFALL":     func(c opensslCipher) bool { return c.enc == "None" },
	"COMPLEMENTOFDEFAULT": func(c opensslCipher) bool { return c.notDefault },
	"HIGH":                func(c opensslCipher) bool { return c.enc != "None" },
	"MEDIUM":              func(opensslCipher) bool { return false },
	"LOW":                 func(opensslCipher) bool { return false },
	"EXP":                 func(opensslCipher) bool { return false },
	"EXPORT":              func(opensslCipher) bool { return false },
	"kRSA":                func(c opensslCipher) bool { return c.kx == "RSA" },
	"RSA":                 func(c opensslCipher) bool { return c.kx == "RSA" },
	"aRSA":                func(c opensslCipher) bool { return c.au == "RSA" },
	"kDHE":                func(c opensslCipher) bool { return c.kx == "DH" },
	"kEDH":                func(c opensslCipher) bool { return c.kx == "DH" },
	"DH":                  func(c opensslCipher) bool { return c.kx == "DH" },
	"DHE":                 func(c opensslCipher) bool { return c.kx == "DH" && c.au != "None" },
	"EDH":                 func(c opensslCipher) bool { return c.kx == "DH" && c.au != "None" },
	"ADH":                 func(c opensslCipher) bool { return c.kx == "DH" && c.au == "None" },
	"kECDHE":              func(c opensslCipher) bool { return c.kx == "ECDH" },
	"kEECDH":              func(c opensslCipher) bool { return c.kx == "ECDH" },
	"ECDH":                func(c opensslCipher) bool { return c.kx == "ECDH" },
	"ECDHE":               func(c opensslCipher) bool { return c.kx == "ECDH" && c.au != "None" },
	"EECDH":               func(c opensslCipher) bool { return c.kx == "ECDH" && c.au != "None" },
	"AECDH":               func(c opensslCipher) bool { return c.kx == "ECDH" && c.au == "None" },
	"aDSS":                func(c opensslCipher) bool { return c.au == "DSS" },
	"DSS":                 func(c opensslCipher) bool { return c.au == "DSS" },
	"aECDSA":              func(c opensslCipher) bool { return c.au == "ECDSA" },
	"ECDSA":               func(c opensslCipher) bool { return c.au == "ECDSA" },
	"aNULL":               func(c opensslCipher) bool { return c.au == "None" },
	"eNULL":               func(c opensslCipher) bool { return c.enc == "None" },
	"NULL":                func(c opensslCipher) bool { return c.enc == "None" },
	"aECDH":               func(opensslCipher) bool { return false },
	"kPSK":                func(c opensslCipher) bool { return c.kx == "PSK" },
	"kRSAPSK":             func(c opensslCipher) bool { return c.kx == "RSAPSK" },
	"kDHEPSK":             func(c opensslCipher) bool { return c.kx == "DHEPSK" },
	"kECDHEPSK":           func(c opensslCipher) bool { return c.kx == "ECDHEPSK" },
	"aPSK":                func(c opensslCipher) bool { return c.au == "PSK" },
	"PSK":                 func(c opensslCipher) bool { return strings.HasSuffix(c.kx, "PSK") },
	"kSRP":                func(c opensslCipher) bool { return c.kx == "SRP" },
	"SRP":                 func(c opensslCipher) bool { return c.kx == "SRP" },
	"aSRP":                func(c opensslCipher) bool { return c.au == "SRP" },
	"AES":                 func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "AES") },
	"AES128": func(c opensslCipher) bool {
		return strings.HasPrefix(c.enc, "AES") && strings.HasSuffix(c.enc, "(128)")
	},
	"AES256": func(c opensslCipher) bool {
		return strings.HasPrefix(c.enc, "AES") && strings.HasSuffix(c.enc, "(256)")
	},
	"AESGCM":      func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "AESGCM") },
	"AESCCM":      func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "AESCCM") },
	"AESCCM8":     func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "AESCCM8") },
	"CHACHA20":    func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "CHACHA20") },
	"CAMELLIA":    func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "Camellia") },
	"CAMELLIA128": func(c opensslCipher) bool { return c.enc == "Camellia(128)" },
	"CAMELLIA256": func(c opensslCipher) bool { return c.enc == "Camellia(256)" },
	"ARIA":        func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "ARIA") },
	"ARIAGCM":     func(c opensslCipher) bool { return strings.HasPrefix(c.enc, "ARIAGCM") },
	"ARIA128":     func(c opensslCipher) bool { return c.enc == "ARIAGCM(128)" },
	"ARIA256":     func(c opensslCipher) bool { return c.enc == "ARIAGCM(256)" },
	"3DES":        func(opensslCipher) bool { return false },
	"DES":         func(opensslCipher) bool { return false },
	"RC4":         func(opensslCipher) bool { return false },
	"RC2":         func(opensslCipher) bool { return false },
	"IDEA":        func(opensslCipher) bool { return false },
	"SEED":        func(opensslCipher) bool { return false },
	"MD5":         func(c opensslCipher) bool { return c.mac == "MD5" },
	"SHA1":        func(c opensslCipher) bool { return c.mac == "SHA1" },
	"SHA":         func(c opensslCipher) bool { return c.mac == "SHA1" },
	"SHA256":      func(c opensslCipher) bool { return c.mac == "SHA256" },
	"SHA384":      func(c opensslCipher) bool { return c.mac == "SHA384" },
	"SSLv3":       func(c opensslCipher) bool { return c.version == "SSLv3" },
	"TLSv1":       func(c opensslCipher) bool { return c.version == "TLSv1" },
	"TLSv1.0":     func(c opensslCipher) bool { return c.version == "TLSv1" },
	"TLSv1.2":     func(c opensslCipher) bool { return c.version == "TLSv1.2" },
}

// opensslDefaultCipherList is OSSL_default_cipher_list().
const opensslDefaultCipherList = "ALL:!COMPLEMENTOFDEFAULT:!eNULL"

// evalCipherList evaluates an OpenSSL cipher list: the crypto/tls suites
// it selects, and whether it selects any cipher at all (OpenSSL rejects a
// list without one below TLS 1.3: "no cipher match").
func evalCipherList(list string) (suites []uint16, any bool) {
	table := cipherTable()
	active := make([]bool, len(table))
	killed := make([]bool, len(table))

	if rest, ok := strings.CutPrefix(list, "DEFAULT"); ok {
		list = opensslDefaultCipherList + ":" + strings.TrimLeft(rest, ":, ")
	}

	for rule := range strings.FieldsFuncSeq(list, func(r rune) bool { return r == ':' || r == ',' || r == ' ' || r == ';' }) {
		op := byte(0)
		if rule[0] == '!' || rule[0] == '-' || rule[0] == '+' {
			op, rule = rule[0], rule[1:]
		}

		// @STRENGTH and @SECLEVEL only order the list or restrict by key
		// sizes; a '+' rule only reorders
		if rule == "" || rule[0] == '@' || op == '+' {
			continue
		}

		match := cipherRuleMatcher(rule)
		if match == nil {
			continue
		}

		for i, c := range table {
			if killed[i] || !match(c) {
				continue
			}

			switch op {
			case '!':
				killed[i], active[i] = true, false
			case '-':
				active[i] = false
			default:
				active[i] = true
			}
		}
	}

	for i, c := range table {
		if active[i] {
			any = true

			if c.suite != 0 {
				suites = append(suites, c.suite)
			}
		}
	}

	return suites, any
}

// cipherRuleMatcher is what a rule selects: a cipher by name, or the
// ciphers every "+"-joined alias selects; nil when a part is unknown (the
// rule then selects nothing, as in OpenSSL).
func cipherRuleMatcher(rule string) func(opensslCipher) bool {
	if !strings.Contains(rule, "+") {
		for _, c := range cipherTable() {
			if c.name == rule {
				return func(o opensslCipher) bool { return o.name == rule }
			}
		}
	}

	var parts []func(opensslCipher) bool

	for part := range strings.SplitSeq(rule, "+") {
		alias, ok := cipherAliases[part]
		if !ok {
			return nil
		}

		parts = append(parts, alias)
	}

	return func(c opensslCipher) bool {
		for _, p := range parts {
			if !p(c) {
				return false
			}
		}

		return true
	}
}
