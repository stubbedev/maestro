// Helpers shared by the drivers and VcsRepository.

package vcs

import (
	"errors"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

// matches is Preg::isMatch($re, $s). Like the other ports, a PCRE failure
// (only backtracking limits) reads as no match.
func matches(re *php.Regexp, s string) bool {
	ok, _ := re.IsMatch(s)

	return ok
}

// match is Preg::match($re, $s, $match): nil when re does not match.
func match(re *php.Regexp, s string) *php.Match {
	m, _ := re.Match(s)

	return m
}

// replace is Preg::replace($re, $replacement, $s).
func replace(re *php.Regexp, replacement, s string) string {
	out, _, err := re.Replace(s, replacement, -1)
	if err != nil {
		return s
	}

	return out
}

// asTransportError is `$e instanceof TransportException`.
func asTransportError(err error) (*util.TransportError, bool) {
	return errors.AsType[*util.TransportError](err)
}

// isTransportError is `$e instanceof TransportException`.
func isTransportError(err error) bool {
	_, ok := asTransportError(err)

	return ok
}

// dummyResponse is new Response(['url' => 'dummy'], 200, [], 'null'),
// what the API drivers return once they fell back to git.
func dummyResponse() *http.Response { return http.NewResponse("dummy", 200, []string{}, "null") }

// arrayPath is $a[$k1][$k2]...: nil when a key is missing or a value on
// the way is not an array.
func arrayPath(a *php.Array, keys ...string) any {
	var v any = a

	for _, k := range keys {
		arr, ok := v.(*php.Array)
		if !ok {
			return nil
		}

		v, _ = arr.Get(k)
	}

	return v
}

// pathString is (string) $a[$k1][$k2]...
func pathString(a *php.Array, keys ...string) string {
	return php.ToString(arrayPath(a, keys...))
}

// isset is isset($a[$k1][$k2]...).
func isset(a *php.Array, keys ...string) bool { return arrayPath(a, keys...) != nil }

// searchLabel is `array_search($needle, $haystack, $strict) ?: $fallback`
// for the first truthy result among haystacks: the key as a string.
func searchLabel(needle string, strict bool, fallback string, haystacks ...func() (*php.Array, error)) (string, error) {
	for _, haystack := range haystacks {
		h, err := haystack()
		if err != nil {
			return "", err
		}

		if k, ok := php.ArraySearch(needle, h, strict); ok && php.ToBool(k.Value()) {
			return k.String(), nil
		}
	}

	return fallback, nil
}

// supportArray is $composer['support'] for writing into it: created when
// missing (PHP auto-vivifies it).
func supportArray(composer *php.Array) *php.Array {
	support, ok := composer.GetArray("support")
	if !ok {
		support = php.NewArray()
		composer.Set("support", support)
	}

	return support
}

// fixSupport is `if (isset($composer['support']) &&
// !is_array($composer['support'])) $composer['support'] = [];`.
func fixSupport(composer *php.Array) {
	if v, _ := composer.Get("support"); v != nil {
		if _, ok := v.(*php.Array); !ok {
			composer.Set("support", php.NewArray())
		}
	}
}
