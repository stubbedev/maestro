package autoload

import (
	"strings"
	"testing"
)

// composer.phar's Compiler::addFile wraps LICENSE in line feeds, and
// AutoloadGenerator::dump copies the phar's file to vendor/composer/LICENSE.
func TestLicense_IsThePharsCopy(t *testing.T) {
	if !strings.HasPrefix(License, "\nCopyright (c) Nils Adermann, Jordi Boggiano\n") {
		t.Fatalf("License starts with %q", License[:min(len(License), 60)])
	}

	if !strings.HasSuffix(License, "THE SOFTWARE.\n\n") {
		t.Fatalf("License ends with %q", License[max(0, len(License)-30):])
	}
}
