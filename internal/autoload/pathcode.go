// Ports AutoloadGenerator::getPathCode, plus what PHP computes when
// getStaticFile requires the generated autoload_*.php files: the values of
// $vendorDir, $baseDir and every path expression in them.

package autoload

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/fspath"
)

// pathBase is the variable a path code is relative to.
type pathBase uint8

const (
	baseNone   pathBase = iota // an absolute path
	baseVendor                 // $vendorDir . '...'
	baseApp                    // $baseDir . '...'
)

// pathRef is a path as getPathCode renders it: an optional phar://
// prefix, the variable it is relative to and the rest of the path.
type pathRef struct {
	phar bool
	base pathBase
	path string
}

// pathRefOf ports getPathCode's decisions for path.
func pathRefOf(basePath, vendorPath, path string) (pathRef, error) {
	if !fspath.IsAbsolutePath(path) {
		path = basePath + "/" + path
	}
	path = fspath.NormalizePath(path)

	var ref pathRef
	if isPathPrefix(path, vendorPath) {
		ref.base = baseVendor
		ref.path = path[len(vendorPath):]
	} else {
		shortest, err := util.FindShortestPath(basePath, path, true, false)
		if err != nil {
			return pathRef{}, err
		}
		ref.path = fspath.NormalizePath(shortest)
		if !fspath.IsAbsolutePath(ref.path) {
			ref.base = baseApp
			ref.path = "/" + ref.path
		}
	}
	ref.phar = isPharPath(ref.path)

	return ref, nil
}

// appendCode appends the PHP code of r, as getPathCode returns it.
func (r pathRef) appendCode(b []byte) []byte {
	if r.phar {
		b = append(b, "'phar://' . "...)
	}
	switch r.base {
	case baseVendor:
		b = append(b, "$vendorDir . "...)
	case baseApp:
		b = append(b, "$baseDir . "...)
	}

	return php.AppendVarExport(b, r.path)
}

// code returns the PHP code of r.
func (r pathRef) code() string { return string(r.appendCode(nil)) }

// value returns what the code of r evaluates to, given $vendorDir and
// $baseDir.
func (r pathRef) value(vendorDir, baseDir string) string {
	prefix := ""
	switch r.base {
	case baseVendor:
		prefix = vendorDir
	case baseApp:
		prefix = baseDir
	}
	if r.phar {
		return "phar://" + prefix + r.path
	}

	return prefix + r.path
}

// isPathPrefix is strpos($path.'/', $prefix.'/') === 0.
func isPathPrefix(path, prefix string) bool {
	return strings.HasPrefix(path, prefix) && (len(path) == len(prefix) || path[len(prefix)] == '/')
}

// isPharPath is Preg::isMatch('{\.phar([\\/]|$)}', $path): ".phar" followed
// by a separator, the end or a final newline.
func isPharPath(path string) bool {
	for i := 0; ; {
		j := strings.Index(path[i:], ".phar")
		if j < 0 {
			return false
		}
		rest := path[i+j+5:]
		if rest == "" || rest == "\n" || rest[0] == '/' || rest[0] == '\\' {
			return true
		}
		i += j + 1
	}
}

// evalPathCode evaluates the PHP code findShortestPathCode returns (with
// __DIR__ possibly renamed $vendorDir), where __DIR__ is dir and $vendorDir
// is vendorDir: a concatenation of __DIR__, $vendorDir, dirname(...) calls
// and var_export'ed strings.
func evalPathCode(code, dir, vendorDir string) string {
	e := pathCodeEvaluator{code: code, dir: dir, vendorDir: vendorDir}

	return e.concat()
}

// concat evaluates a concatenation from the current position.
func (e *pathCodeEvaluator) concat() string {
	var b strings.Builder
	for {
		b.WriteString(e.term())
		e.skipSpaces()
		if e.pos >= len(e.code) || e.code[e.pos] != '.' {
			return b.String()
		}
		e.pos++
		e.skipSpaces()
	}
}

type pathCodeEvaluator struct {
	code                    string
	pos                     int
	dir, vendorDir, baseDir string
}

func (e *pathCodeEvaluator) skipSpaces() {
	for e.pos < len(e.code) && e.code[e.pos] == ' ' {
		e.pos++
	}
}

// term evaluates one operand of the concatenation.
func (e *pathCodeEvaluator) term() string {
	rest := e.code[e.pos:]
	switch {
	case strings.HasPrefix(rest, "__DIR__"):
		e.pos += len("__DIR__")

		return e.dir
	case strings.HasPrefix(rest, "$vendorDir"):
		e.pos += len("$vendorDir")

		return e.vendorDir
	case strings.HasPrefix(rest, "$baseDir"):
		e.pos += len("$baseDir")

		return e.baseDir
	case strings.HasPrefix(rest, "dirname("):
		e.pos += len("dirname(")
		inner := e.term()
		e.pos++ // )

		return php.Dirname(inner)
	case strings.HasPrefix(rest, `"\0"`):
		e.pos += 4

		return "\x00"
	case strings.HasPrefix(rest, "'"):
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			switch c := rest[i]; {
			case c == '\'':
				e.pos += i + 1

				return b.String()
			case c == '\\' && i+1 < len(rest) && (rest[i+1] == '\\' || rest[i+1] == '\''):
				b.WriteByte(rest[i+1])
				i++
			default:
				b.WriteByte(c)
			}
		}
	}
	panic("autoload: unexpected path code " + e.code)
}
