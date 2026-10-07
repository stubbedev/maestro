// PHP 8's sprintf() (internal/php's port of php_formatted_print()) as the
// console throws it, used for ProgressBar placeholders ("%percent:3s%"), Table
// formats, ChoiceQuestion error messages and the synopsis rendered after an
// exception.

package console

import (
	"github.com/stubbedev/maestro/internal/php"
)

// phpSprintf formats args like PHP 8's sprintf(). A bad format panics with
// the *php.EngineError PHP throws (a ValueError or ArgumentCountError).
func phpSprintf(format string, args ...any) string {
	s, err := php.Sprintf(format, args...)
	if err != nil {
		panic(err)
	}

	return s
}
