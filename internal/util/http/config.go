// The parts of Composer\Config (src/Composer/Config.php) and
// Composer\Config\ConfigSourceInterface the HTTP layer uses, as interfaces
// internal/config implements, plus helpers reading Composer's free-form
// option arrays.

package http

import (
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

// configHas is in_array($value, $config->get($key), true).
func configHas(config Config, key, value string) bool {
	a, _ := config.Get(key).(*php.Array)

	return php.InArray(value, a, true)
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

type AccessCallable func(string) (bool, error)

func (AccessCallable) PHPOpaque() {}

func (AccessCallable) PHPClass() string { return "Closure" }

func accessCallable(options *php.Array, name string) (AccessCallable, bool) {
	v, ok := path(options, name)
	if !ok {
		return nil, false
	}

	fn, ok := v.(AccessCallable)

	return fn, ok
}
