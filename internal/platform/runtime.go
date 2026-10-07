// Ports src/Composer/Platform/Runtime.php.

package platform

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

// Runtime ports Composer\Platform\Runtime, the questions Composer asks the
// PHP it runs on. Composer answers them in-process; maestro answers them
// from a Snapshot taken by running the user's php once (NewRuntime). It is
// an interface so that tests mock it as Composer's tests mock the class.
//
// Values are PHP values as internal/php represents them (nil, bool, int64,
// float64, string, *php.Array) or the objects below. Where PHP throws, the
// methods return a *PHPError with PHP's class and message.
type Runtime interface {
	// HasConstant ports Runtime::hasConstant: defined($class::$constant),
	// or defined($constant) for an empty class.
	HasConstant(constant, class string) bool
	// GetConstant ports Runtime::getConstant: constant(...).
	GetConstant(constant, class string) (any, error)
	// HasFunction ports Runtime::hasFunction: function_exists($fn).
	HasFunction(fn string) bool
	// Invoke ports Runtime::invoke: $callable(...$arguments).
	Invoke(callable Callable, arguments ...any) (any, error)
	// HasClass ports Runtime::hasClass: class_exists($class, false).
	HasClass(class string) bool
	// Construct ports Runtime::construct: new $class(...$arguments).
	Construct(class string, arguments ...any) (any, error)
	// GetExtensions ports Runtime::getExtensions: get_loaded_extensions().
	GetExtensions() []string
	// GetExtensionVersion ports Runtime::getExtensionVersion:
	// phpversion($extension), "0" when that is false.
	GetExtensionVersion(extension string) string
	// GetExtensionInfo ports Runtime::getExtensionInfo: what
	// ReflectionExtension::info() prints, in the CLI SAPI's text format.
	GetExtensionInfo(extension string) (string, error)
}

// Callable is a PHP callable as Runtime::invoke receives it: a function
// name ("inet_pton") or a static method (['ResourceBundle', 'create']).
type Callable struct {
	Class, Name string
}

// Func is the callable naming function name.
func Func(name string) Callable { return Callable{Name: name} }

// StaticMethod is the callable [class, method].
func StaticMethod(class, method string) Callable { return Callable{Class: class, Name: method} }

// String is the callable as PHP names it in messages: "fn" or
// "Class::method".
func (c Callable) String() string {
	if c.Class == "" {
		return c.Name
	}

	return c.Class + "::" + c.Name
}

// ResourceBundle is the part of PHP's ResourceBundle that Composer uses:
// what ResourceBundle::create returns, when it is not null.
type ResourceBundle interface {
	Get(field any) (any, error)
}

// Imagick is the part of PHP's Imagick that Composer uses.
type Imagick interface {
	GetVersion() (any, error)
}

// PHPError is a Throwable raised by PHP: its class ("Error",
// "ReflectionException", ...) and message.
type PHPError struct {
	Class, Message string
}

func (e *PHPError) Error() string { return e.Message }

// PHPClass implements phperr.Exception: get_class($e).
func (e *PHPError) PHPClass() string { return e.Class }

var (
	extensionInfoTitleRe = php.MustCompile(`~<h2>\s*<a[^>]*>([^<]+)</a>\s*</h2>~i`)
	extensionInfoRowRe   = php.MustCompile(`~<tr>\s*<td class="e">\s*(.*?)\s*</td>\s*<td class="v">\s*(.*?)\s*</td>\s*</tr>~is`)
)

// ParseHtmlExtensionInfo ports Runtime::parseHtmlExtensionInfo, which turns
// the HTML ReflectionExtension::info() prints outside the CLI SAPI into the
// CLI's text format. maestro only runs the CLI, so it is used by tests.
// The error is the PcreException Preg throws.
func ParseHtmlExtensionInfo(html string) (string, error) {
	var result []string

	m, err := extensionInfoTitleRe.Match(html)
	if err != nil {
		return "", err
	}
	if m != nil {
		result = append(result, php.Trim(php.HTMLEntityDecode(m.Get(1))), "")
	}

	matches, err := extensionInfoRowRe.MatchAll(html)
	if err != nil {
		return "", err
	}
	if len(matches) > 0 {
		for _, m := range matches {
			key := php.Trim(php.HTMLEntityDecode(php.StripTags(m.Get(1))))
			value := php.Trim(php.HTMLEntityDecode(php.StripTags(m.Get(2))))
			result = append(result, key+" => "+value)
		}
	}

	return strings.Join(result, "\n"), nil
}
