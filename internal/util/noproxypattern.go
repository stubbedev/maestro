// Ports src/Composer/Util/NoProxyPattern.php.

package util

import (
	"bytes"
	"net/netip"
	"strconv"
	"strings"
)

// NoProxyPattern ports Composer\Util\NoProxyPattern: tests URLs against a
// NO_PROXY pattern.
type NoProxyPattern struct {
	hostNames []string
	rules     []*noProxyRule
	parsed    []bool
	noproxy   bool
}

type noProxyRule struct {
	name   string
	port   int
	ipdata *noProxyIPData
}

type noProxyIPData struct {
	ip      []byte // in_addr, IPv4 mapped to IPv6
	size    int    // byte size of the original in_addr
	netmask []byte // nil unless a CIDR prefix was given
}

// NewNoProxyPattern parses a NO_PROXY value: host names, IP addresses and
// CIDR ranges with optional ports, separated by whitespace or commas.
func NewNoProxyPattern(pattern string) *NoProxyPattern {
	hostNames := strings.FieldsFunc(pattern, func(r rune) bool {
		return r == ',' || (r < 0x80 && isPCRESpace(byte(r)))
	})

	return &NoProxyPattern{
		hostNames: hostNames,
		rules:     make([]*noProxyRule, len(hostNames)),
		parsed:    make([]bool, len(hostNames)),
		noproxy:   len(hostNames) == 0 || hostNames[0] == "*",
	}
}

// Test ports NoProxyPattern::test: whether url matches the pattern.
func (n *NoProxyPattern) Test(url string) bool {
	if n.noproxy {
		return true
	}

	urlData := getURLData(url)
	if urlData == nil {
		return false
	}

	for index, hostName := range n.hostNames {
		if n.match(index, hostName, urlData) {
			return true
		}
	}

	return false
}

// getURLData returns the data of url, or nil when it cannot be parsed.
func getURLData(url string) *noProxyRule {
	u, ok := parseURL(url)

	// if (!$host = parse_url(...)): "0" is falsy too.
	if !ok || !phpTruthy(u.host) {
		return nil
	}

	port := 0
	if u.hasPort {
		port = u.port
	}

	if port == 0 {
		switch strings.ToLower(u.scheme) {
		case "http":
			port = 80
		case "https":
			port = 443
		}
	}

	hostName := u.host
	if port != 0 {
		hostName += ":" + strconv.Itoa(port)
	}

	host, port, ok := splitHostPort(hostName)
	if !ok {
		return nil
	}

	ipdata, ok := ipCheckData(host, false)
	if !ok {
		return nil
	}

	return makeNoProxyData(host, port, ipdata)
}

// match ports NoProxyPattern::match.
func (n *NoProxyPattern) match(index int, hostName string, url *noProxyRule) bool {
	rule := n.getRule(index, hostName)
	if rule == nil {
		// Data must have been misformatted.
		return false
	}

	var matched bool

	if rule.ipdata != nil {
		// Match ipdata first.
		if url.ipdata == nil {
			return false
		}

		if rule.ipdata.netmask != nil {
			return matchRange(rule.ipdata, url.ipdata)
		}

		matched = bytes.Equal(rule.ipdata.ip, url.ipdata.ip)
	} else {
		// Match host and port: substr($url->name, -strlen($rule->name)).
		haystack := url.name
		if len(rule.name) < len(haystack) {
			haystack = haystack[len(haystack)-len(rule.name):]
		}

		matched = hasPrefixFold(haystack, rule.name)
	}

	if matched && rule.port != 0 {
		matched = rule.port == url.port
	}

	return matched
}

// matchRange reports whether target is in the network range.
func matchRange(network, target *noProxyIPData) bool {
	for i := range 16 {
		if network.ip[i]&network.netmask[i] != target.ip[i]&network.netmask[i] {
			return false
		}
	}

	return true
}

// getRule finds or creates the rule for a host name; nil when it is
// invalid.
func (n *NoProxyPattern) getRule(index int, hostName string) *noProxyRule {
	if n.parsed[index] {
		return n.rules[index]
	}

	n.parsed[index] = true

	host, port, ok := splitHostPort(hostName)
	if !ok {
		return nil
	}

	ipdata, ok := ipCheckData(host, true)
	if !ok {
		return nil
	}

	n.rules[index] = makeNoProxyData(host, port, ipdata)

	return n.rules[index]
}

// ipCheckData ports NoProxyPattern::ipCheckData: the IP data when host is
// an IP address (with a CIDR prefix-length if allowPrefix), nil for a host
// name, and false when host holds invalid data.
func ipCheckData(host string, allowPrefix bool) (*noProxyIPData, bool) {
	prefix := -1
	modified := false

	// Check for a CIDR prefix-length.
	if i := strings.IndexByte(host, '/'); i >= 0 {
		// [$host, $prefix] = explode('/', $host)
		prefixStr := host[i+1:]
		if j := strings.IndexByte(prefixStr, '/'); j >= 0 {
			prefixStr = prefixStr[:j]
		}

		host = host[:i]

		value, ok := filterValidateInt(prefixStr, 0, 128)
		if !allowPrefix || !ok {
			return nil, false
		}

		prefix = value
		modified = true
	}

	// See if this is an IP address.
	if !filterValidateIP(host) {
		return nil, !modified
	}

	ip, size := ipGetAddr(host)

	var netmask []byte

	if prefix >= 0 {
		// Check for a valid prefix.
		if prefix > size*8 {
			return nil, false
		}

		ip, netmask = ipGetNetwork(ip, size, prefix)
	}

	return &noProxyIPData{ip: ip, size: size, netmask: netmask}, true
}

// ipGetAddr returns the in_addr of an IP mapped to IPv6, and its original
// byte size.
func ipGetAddr(host string) ([]byte, int) {
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return make([]byte, 16), 16
	}

	size := 16
	if addr.Is4() {
		size = 4
	}

	b := addr.As16()

	return b[:], size
}

// ipGetMask returns the binary network mask for a prefix, mapped to IPv6.
func ipGetMask(prefix, size int) []byte {
	mask := make([]byte, 0, 16)
	for range prefix / 8 {
		mask = append(mask, 0xff)
	}

	if remainder := prefix % 8; remainder != 0 {
		mask = append(mask, 0xff^(0xff>>remainder))
	}

	for len(mask) < size {
		mask = append(mask, 0)
	}

	return ipMapTo6(mask, size)
}

// ipGetNetwork returns the network of ip and the mask for the prefix.
func ipGetNetwork(rangeIP []byte, size, prefix int) ([]byte, []byte) {
	netmask := ipGetMask(prefix, size)
	network := make([]byte, 16)

	for i := range 16 {
		network[i] = rangeIP[i] & netmask[i]
	}

	return network, netmask
}

// ipMapTo6 maps a 4 byte in_addr to IPv6.
func ipMapTo6(binary []byte, size int) []byte {
	if size == 4 {
		return append([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff}, binary...)
	}

	return binary
}

// makeNoProxyData ports NoProxyPattern::makeData.
func makeNoProxyData(host string, port int, ipdata *noProxyIPData) *noProxyRule {
	return &noProxyRule{name: "." + strings.TrimLeft(host, "."), port: port, ipdata: ipdata}
}

// splitHostPort ports NoProxyPattern::splitHostPort: host and port, with
// IPv6 addresses in square brackets.
func splitHostPort(hostName string) (string, int, bool) {
	port := 0
	ip6 := ""

	// Check for square-bracket notation.
	if hostName != "" && hostName[0] == '[' {
		index := strings.IndexByte(hostName, ']')

		// The smallest ip6 address is ::
		if index < 3 {
			return "", 0, false
		}

		ip6 = hostName[1:index]
		hostName = hostName[index+1:]

		if strings.ContainsAny(hostName, "[]") || strings.Count(hostName, ":") > 1 {
			return "", 0, false
		}
	}

	if strings.Count(hostName, ":") == 1 {
		index := strings.IndexByte(hostName, ':')
		portStr := hostName[index+1:]
		hostName = hostName[:index]

		value, ok := filterValidateInt(portStr, 1, 65535)
		if !ok {
			return "", 0, false
		}

		port = value
	}

	return ip6 + hostName, port, true
}
