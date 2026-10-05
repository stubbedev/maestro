package json

import (
	"os"
	"testing"
)

// BenchmarkJsonManipulator_AddLink adds a requirement to Composer's own
// composer.json, as `composer require` does.
func BenchmarkJsonManipulator_AddLink(b *testing.B) {
	contents, err := os.ReadFile("../../.ref/composer/composer.json")
	if err != nil {
		b.Skip(".ref/composer is not synced")
	}
	b.ReportAllocs()
	for b.Loop() {
		m, err := NewManipulator(string(contents))
		if err != nil {
			b.Fatal(err)
		}
		if ok, err := m.AddLink("require", "vendor/new-package", "^1.0", true); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}
