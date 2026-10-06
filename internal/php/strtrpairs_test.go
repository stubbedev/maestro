package php

import (
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

// strtrPairsNaive is strtr($s, $pairs) tried at every position.
func strtrPairsNaive(s string, pairs map[string]string) string {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	var b strings.Builder
	for i := 0; i < len(s); {
		found := false
		for _, k := range keys {
			if strings.HasPrefix(s[i:], k) {
				b.WriteString(pairs[k])
				i += len(k)
				found = true

				break
			}
		}
		if !found {
			b.WriteByte(s[i])
			i++
		}
	}

	return b.String()
}

func TestStrtrPairs_MatchesNaive(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	word := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = "ab =>'"[r.IntN(6)]
		}

		return string(b)
	}
	for range 20000 {
		pairs := map[string]string{}
		prefix := word(r.IntN(3))
		for range 1 + r.IntN(4) {
			pairs[prefix+word(r.IntN(4))] = word(r.IntN(3))
		}
		s := word(r.IntN(40))
		if got, want := StrtrPairs(s, pairs), strtrPairsNaive(s, pairs); got != want {
			t.Fatalf("StrtrPairs(%q, %q) = %q, want %q", s, pairs, got, want)
		}
	}
}
