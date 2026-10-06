package classmap

import (
	"math/rand/v2"
	"testing"
)

// countTypeKeywordsNaive tries the pattern at every position.
func countTypeKeywordsNaive(c []byte, enums bool) int {
	n := 0
	for i := 0; i < len(c); i++ {
		if !typeStart[c[i]] || i > 0 && isWordChar[c[i-1]] {
			continue
		}
		kw := typeKeyword(c, i, enums)
		if kw == "" || kw == "namespace" {
			continue
		}
		end := i + len(kw)
		if end < len(c) && isPcreSpace[c[end]] {
			n++
			if n == 2 {
				return n
			}
			i = end
		}
	}

	return n
}

func TestCountTypeKeywords_MatchesNaive(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	parts := []string{"class", "CLASS", "interface", "trait", "enum", "namespace", " ", "\n", "_", "a", "x", "$", "\\", "e", "\xff", "classes", "Enum"}
	for range 50000 {
		var b []byte
		for range r.IntN(8) {
			b = append(b, parts[r.IntN(len(parts))]...)
		}
		enums := r.IntN(2) == 0
		if got, want := countTypeKeywords(b, enums), countTypeKeywordsNaive(b, enums); got != want {
			t.Fatalf("countTypeKeywords(%q, %v) = %d, want %d", b, enums, got, want)
		}
	}
}
