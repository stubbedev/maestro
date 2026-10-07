// The parts of Composer\Config (src/Composer/Config.php) and
// Composer\Config\ConfigSourceInterface the HTTP layer uses, as interfaces
// internal/config implements, plus helpers reading Composer's free-form
// option arrays.

package http

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
)

// Config is the part of Composer\Config the HTTP layer reads. Get returns
// values in the internal/php value model (lists and maps are *php.Array),
// like io.Config.
type Config interface {
	Get(key string) any
	// ProhibitURLByConfig is Config::prohibitUrlByConfig($url, $io,
	// $repoOptions): a *util.TransportError when the configuration forbids
	// accessing url. io may be nil.
	ProhibitURLByConfig(url string, io io.IO, repoOptions *php.Array) error
	ConfigSource() ConfigSource
	AuthConfigSource() ConfigSource
	// LocalAuthConfigSource is getLocalAuthConfigSource(); nil for null.
	LocalAuthConfigSource() ConfigSource
}

// ConfigSource is the part of Composer\Config\ConfigSourceInterface the
// HTTP layer writes credentials through.
type ConfigSource interface {
	Name() string
	AddConfigSetting(name string, value any) error
	RemoveConfigSetting(name string) error
}

// configList is $config->get($key) for a list of strings (github-domains,
// gitlab-domains, ...).
func configList(config Config, key string) []string {
	a, ok := config.Get(key).(*php.Array)
	if !ok {
		return nil
	}

	out := make([]string, 0, a.Len())
	for _, v := range a.All() {
		out = append(out, php.ToString(v))
	}

	return out
}

// configHas is in_array($value, $config->get($key), true).
func configHas(config Config, key, value string) bool {
	a, ok := config.Get(key).(*php.Array)
	if !ok {
		return false
	}

	for _, v := range a.All() {
		if s, ok := v.(string); ok && s == value {
			return true
		}
	}

	return false
}

// path walks a nested option array: $options[$k1][$k2]... with isset()
// semantics (a missing key or a null value report false).
func path(options *php.Array, keys ...string) (any, bool) {
	var cur any = options
	for _, k := range keys {
		a, ok := cur.(*php.Array)
		if !ok || a == nil {
			return nil, false
		}

		cur, ok = a.Get(k)
		if !ok {
			return nil, false
		}
	}

	return cur, cur != nil
}

// HTTPOptions returns $options['http'] as an array, creating it (and
// replacing a non-array value) when needed, as PHP's
// $options['http'][...] = ... does.
func HTTPOptions(options *php.Array) *php.Array {
	if v, ok := options.Get("http"); ok {
		if a, ok := v.(*php.Array); ok {
			return a
		}
	}

	a := php.NewArray()
	options.Set("http", a)

	return a
}

// headerList returns $options['http']['header'] as a list of lines: a
// string is split on "\r\n" like the stream context code does.
func headerList(options *php.Array) []string {
	v, ok := path(options, "http", "header")
	if !ok {
		return nil
	}

	switch h := v.(type) {
	case string:
		return splitCRLF(h)
	case *php.Array:
		out := make([]string, 0, h.Len())
		for _, line := range h.All() {
			out = append(out, php.ToString(line))
		}

		return out
	}

	return []string{php.ToString(v)}
}

// splitCRLF is explode("\r\n", $s).
func splitCRLF(s string) []string {
	var out []string
	for {
		i := indexCRLF(s)
		if i < 0 {
			return append(out, s)
		}

		out = append(out, s[:i])
		s = s[i+2:]
	}
}

func indexCRLF(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '\r' && s[i+1] == '\n' {
			return i
		}
	}

	return -1
}

// optionString is (string) $options[...] when set.
func optionString(options *php.Array, keys ...string) (string, bool) {
	v, ok := path(options, keys...)
	if !ok {
		return "", false
	}

	return php.ToString(v), true
}

// Callables cannot live in a *php.Array, so the prevent_url_access_callable
// and prevent_ip_access_callable options hold a handle RegisterCallable
// returned instead.
var (
	callables   sync.Map // string -> func(string) bool
	callableSeq atomic.Uint64
)

const callablePrefix = "\x00maestro-callable:"

// RegisterCallable registers fn for use as a callable option value
// (prevent_url_access_callable, prevent_ip_access_callable) and returns
// the value to put in the options.
func RegisterCallable(fn func(string) bool) string {
	key := callablePrefix + strconv.FormatUint(callableSeq.Add(1), 10)
	callables.Store(key, fn)

	return key
}

// callableOption resolves a callable option value; false when it is not
// callable (is_callable() false).
func callableOption(v any) (func(string) bool, bool) {
	key, ok := v.(string)
	if !ok || !strings.HasPrefix(key, callablePrefix) {
		return nil, false
	}

	fn, ok := callables.Load(key)
	if !ok {
		return nil, false
	}

	f, ok := fn.(func(string) bool)

	return f, ok
}
