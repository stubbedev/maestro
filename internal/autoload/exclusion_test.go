package autoload

import (
	"math/rand/v2"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// TestConstantPrefixIsComposersReplace checks constantPrefix against the
// regex Composer runs (buildExclusionRegex), on real patterns and on
// random strings of the bytes that steer it.
func TestConstantPrefixIsComposersReplace(t *testing.T) {
	re := php.MustCompile(`{^(([^.+*?\[^\]$(){}=!<>|:\\#-]+|\\[.+*?\[^\]$(){}=!<>|:#-])*).*}`)
	check := func(pattern string) {
		t.Helper()
		want, _, err := re.Replace(pattern, "$1", -1)
		if err != nil {
			t.Fatalf("%q: %v", pattern, err)
		}
		if got := constantPrefix(pattern); got != want {
			t.Fatalf("constantPrefix(%q) = %q, want %q", pattern, got, want)
		}
	}

	for _, p := range []string{
		"", "/app/vendor/symfony/console/Tests/", `/app/vendor/a\-b/Tests/`, `/app/vendor/x/[^/]*/Tests/`,
		`/app/vendor/x/.+/Tests($|/)`, `\\`, `\`, `a\`, `a\b`, "a\nb", "a.b\nc\nd", ".\n", `\.\(\)`,
	} {
		check(p)
	}

	alphabet := []byte("ab/\\\n.+*?[^]$(){}=!<>|:#-")
	rng := rand.New(rand.NewPCG(1, 2))
	for range 20000 {
		b := make([]byte, rng.IntN(12))
		for i := range b {
			b[i] = alphabet[rng.IntN(len(alphabet))]
		}
		check(string(b))
	}
}
