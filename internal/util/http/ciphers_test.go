package http

import (
	"crypto/tls"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

var cipherListCases = []string{
	tlsCiphers,
	"DEFAULT",
	"HIGH:!aNULL:+RSA:@STRENGTH",
	"ECDHE+AESGCM:-ECDSA:AES128",
	"ECDHE+AESGCM:!ECDSA:AES128",
	"AES128-SHA",
	"kRSA:!SHA1",
	"ECDHE-ECDSA-CHACHA20-POLY1305:AES256-GCM-SHA384",
	"CHACHA20",
	"aECDSA+SHA256",
	"TLSv1.2+AESGCM:!kRSA",
	"DHE-RSA-AES128-GCM-SHA256",
	"NOPE",
	"ALL:!AES:!CHACHA20",
}

// TestEvalCipherList compares cipher list evaluation with OpenSSL's
// (`openssl ciphers`) on the suites crypto/tls implements.
func TestEvalCipherList(t *testing.T) {
	if _, err := exec.LookPath("openssl"); err != nil {
		t.Skip("openssl not found")
	}

	ids := map[string]uint16{}
	for _, c := range cipherTable() {
		ids[c.name] = c.suite
	}

	for _, list := range cipherListCases {
		out, err := exec.Command("openssl", "ciphers", "-tls1_2", list).Output()

		var want []uint16

		wantAny := err == nil

		for name := range strings.SplitSeq(strings.TrimSpace(string(out)), ":") {
			if id := ids[name]; id != 0 {
				want = append(want, id)
			}
		}

		got, gotAny := evalCipherList(list)
		slices.Sort(got)
		slices.Sort(want)

		if !slices.Equal(got, want) || gotAny != wantAny {
			t.Errorf("%q: got %v %v, want %v %v", list, names(got), gotAny, names(want), wantAny)
		}
	}
}

// TestEvalCipherList_ComposerDefault pins what Composer's stream cipher
// list selects.
func TestEvalCipherList_ComposerDefault(t *testing.T) {
	got, _ := evalCipherList(tlsCiphers)

	want := []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA, tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
		tls.TLS_RSA_WITH_AES_256_GCM_SHA384, tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_RSA_WITH_AES_128_CBC_SHA256, tls.TLS_RSA_WITH_AES_256_CBC_SHA, tls.TLS_RSA_WITH_AES_128_CBC_SHA,
	}

	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("got %v", names(got))
	}
}

func names(ids []uint16) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, tls.CipherSuiteName(id))
	}

	return out
}
