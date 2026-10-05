package jsonschema

import (
	"strings"
	"testing"
)

// preg_grep stops at a matching error and returns the entries collected
// so far, so a property exhausting the backtrack limit does not make the
// style invalid.
func TestValidateStyle_PcreError(t *testing.T) {
	prop := "a:" + strings.Repeat(" ", 3000) + "\n\n"
	if _, err := styleRegexp.IsMatch(prop); err == nil {
		t.Fatal("expected the property to exhaust the backtrack limit")
	}
	if !validateStyle(prop) {
		t.Error("validateStyle = false, want true (preg_grep found no invalid entry before the error)")
	}
	if validateStyle("1;" + prop) {
		t.Error("validateStyle = true, want false (an invalid entry precedes the error)")
	}
}
