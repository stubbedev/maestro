package php

import (
	"strconv"
	"sync/atomic"
	"testing"
)

var lazyTestRuns atomic.Int64

func TestMustCompileLazy(t *testing.T) {
	// a pattern no earlier run of the test compiled
	pattern := `{(a)(?<n>b)?z{0,` + strconv.FormatInt(lazyTestRuns.Add(1), 10) + `}}`
	re := MustCompile(pattern)
	if re.lazy == nil || re.lazy.re != nil {
		t.Fatal("MustCompile compiled the pattern before its first use")
	}
	if re.String() != pattern {
		t.Fatalf("String() = %q", re.String())
	}
	n, a, err := re.MatchAllArray("aab", 0, 0)
	if err != nil || n != 2 || a.Len() != 4 {
		t.Fatalf("MatchAllArray = %d, %v, %v", n, a, err)
	}
	if c, _ := Compile(pattern); c != re.lazy.re {
		t.Fatal("the lazy pattern does not share the regex cache")
	}
	if MustCompile(pattern) != re {
		t.Fatal("a pattern given again is another *Regexp")
	}
}

func TestMustCompileInvalid(t *testing.T) {
	bad := MustCompile(`{(}`)
	defer func() {
		lazyPatterns.Lock()
		delete(lazyPatterns.m, `{(}`)
		lazyPatterns.Unlock()
	}()
	if err := CheckMustCompiled(); err == nil {
		t.Fatal("CheckMustCompiled accepted an invalid pattern")
	}
	defer func() {
		if recover() == nil {
			t.Fatal("using an invalid pattern did not panic")
		}
	}()
	_, _ = bad.IsMatch("x")
}
