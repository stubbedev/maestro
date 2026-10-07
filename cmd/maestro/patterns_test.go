package main

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// php.MustCompile compiles on first use, so an invalid package-level
// pattern would only panic in the run that uses it: compile them all.
func TestPackagePatternsCompile(t *testing.T) {
	if err := php.CheckMustCompiled(); err != nil {
		t.Fatal(err)
	}
}
