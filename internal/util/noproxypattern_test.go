package util

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// Ports tests/Composer/Test/Util/NoProxyPatternTest.php.

type noProxyCase struct {
	name, noproxy, url string
	expected           bool
}

func runNoProxyCases(t *testing.T, cases []noProxyCase) {
	t.Helper()

	for _, c := range cases {
		matcher := NewNoProxyPattern(c.noproxy)
		if got := matcher.Test(noProxyTestURL(c.url)); got != c.expected {
			t.Errorf("%s: Test(%q) with %q = %v, want %v", c.name, c.url, c.noproxy, got, c.expected)
		}
	}
}

// noProxyTestURL ports NoProxyPatternTest::getUrl: it appends a scheme to
// the test url if it is missing.
func noProxyTestURL(url string) string {
	if u, ok := parseURL(url); ok && php.Truthy(u.scheme) {
		return url
	}

	scheme := "http"

	if !strings.HasPrefix(url, "[") && strings.Contains(url, ":") {
		if parts := strings.Split(url, ":"); parts[1] == "443" {
			scheme = "https"
		}
	}

	return scheme + "://" + url
}

func TestNoProxyPattern_HostName(t *testing.T) {
	const noproxy = "foobar.com, .barbaz.net"

	runNoProxyCases(t, []noProxyCase{
		{"match as foobar.com", noproxy, "foobar.com", true},
		{"match foobar.com", noproxy, "www.foobar.com", true},
		{"no match foobar.com", noproxy, "foofoobar.com", false},
		{"match .barbaz.net 1", noproxy, "barbaz.net", true},
		{"match .barbaz.net 2", noproxy, "www.barbaz.net", true},
		{"no match .barbaz.net", noproxy, "barbarbaz.net", false},
		{"no match wrong domain", noproxy, "barbaz.com", false},
		{"no match FQDN", noproxy, "foobar.com.", false},
	})
}

func TestNoProxyPattern_IpAddress(t *testing.T) {
	const noproxy = "192.168.1.1, 2001:db8::52:0:1"

	runNoProxyCases(t, []noProxyCase{
		{"match exact IPv4", noproxy, "192.168.1.1", true},
		{"no match IPv4", noproxy, "192.168.1.4", false},
		{"match exact IPv6", noproxy, "[2001:db8:0:0:0:52:0:1]", true},
		{"no match IPv6", noproxy, "[2001:db8:0:0:0:52:0:2]", false},
		{"match mapped IPv4", noproxy, "[::FFFF:C0A8:0101]", true},
		{"no match mapped IPv4", noproxy, "[::FFFF:C0A8:0104]", false},
	})
}

func TestNoProxyPattern_IpRange(t *testing.T) {
	const noproxy = "10.0.0.0/30, 2002:db8:a::45/121"

	runNoProxyCases(t, []noProxyCase{
		{"match IPv4/CIDR", noproxy, "10.0.0.2", true},
		{"no match IPv4/CIDR", noproxy, "10.0.0.4", false},
		{"match IPv6/CIDR", noproxy, "[2002:db8:a:0:0:0:0:7f]", true},
		{"no match IPv6", noproxy, "[2002:db8:a:0:0:0:0:ff]", false},
		{"match mapped IPv4", noproxy, "[::FFFF:0A00:0002]", true},
		{"no match mapped IPv4", noproxy, "[::FFFF:0A00:0004]", false},
	})
}

func TestNoProxyPattern_Port(t *testing.T) {
	const noproxy = "192.168.1.2:81, 192.168.1.3:80, [2001:db8::52:0:2]:443, [2001:db8::52:0:3]:80"

	runNoProxyCases(t, []noProxyCase{
		{"match IPv4 port", noproxy, "192.168.1.3", true},
		{"no match IPv4 port", noproxy, "192.168.1.2", false},
		{"match IPv6 port", noproxy, "[2001:db8::52:0:3]", true},
		{"no match IPv6 port", noproxy, "[2001:db8::52:0:2]", false},
	})
}
