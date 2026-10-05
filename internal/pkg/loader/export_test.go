package loader

import (
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// Test hooks for the external tests.

func ParseDateTime(s string) (time.Time, error) { return parseDateTime(s) }

func FilterEmail(v any) bool { return filterEmail(v) }

func FilterURL(v any, schemes ...string) (bool, error) { return filterURL(v, schemes...) }

func ExtractAliases(l *RootPackageLoader, requires *php.Array) (*php.Array, error) {
	return l.extractAliases(requires, php.NewArray())
}
