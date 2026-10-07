// Benchmarks of the paths the solver and the loaders hit hardest.

package semver

import (
	"sync"
	"testing"
)

func BenchmarkConstraint_Matches(b *testing.B) {
	require := NewConstraintOp(OpGE, "1.2.3.0-dev")
	provide := NewConstraintOp(OpEQ, "1.10.0.0")
	b.ReportAllocs()
	for b.Loop() {
		require.Matches(provide)
	}
}

func BenchmarkMultiConstraint_Matches(b *testing.B) {
	require, err := VersionParser{}.ParseConstraints("^1.2.3 || ^2.0, !=2.0.5 || ~3.1.0")
	if err != nil {
		b.Fatal(err)
	}
	provide := NewConstraintOp(OpEQ, "3.1.4.0")
	b.ReportAllocs()
	for b.Loop() {
		require.Matches(provide)
	}
}

func BenchmarkCompilingMatcher_Match(b *testing.B) {
	require, err := VersionParser{}.ParseConstraints("^1.2.3 || ^2.0, !=2.0.5 || ~3.1.0")
	if err != nil {
		b.Fatal(err)
	}
	CompilingMatcher.Match(require, OpEQ, "3.1.4.0")
	b.ReportAllocs()
	for b.Loop() {
		CompilingMatcher.Match(require, OpEQ, "3.1.4.0")
	}
}

func BenchmarkVersionCompare(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		VersionCompare("1.10.3.0-beta2", "1.10.3.0-RC1")
	}
}

func BenchmarkVersionCompare_Plain(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		VersionCompare("6.4.12.0", "6.4.3.0")
	}
}

func BenchmarkVersionParser_Normalize(b *testing.B) {
	var p VersionParser
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.Normalize("v1.10.3-beta.2")
	}
}

func BenchmarkVersionParser_ParseStability(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ParseStability("1.10.3.0-beta2")
	}
}

func BenchmarkVersionParser_ParseConstraints(b *testing.B) {
	var p VersionParser
	b.ReportAllocs()
	for b.Loop() {
		_, _ = p.ParseConstraints("^1.2.3 || ^2.0, !=2.0.5 || ~3.1.0")
	}
}

func BenchmarkIntervals_CompactConstraint(b *testing.B) {
	c, err := VersionParser{}.ParseConstraints("^1.2.3 || ^2.0, !=2.0.5 || ~3.1.0 || 1.5.*")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		Intervals.CompactConstraint(c)
	}
}

// The caches are shared by every goroutine of the solver.
func TestConcurrentCaches(t *testing.T) {
	constraints := []string{"^1.2 || ^2.0", "~1.0, !=1.0.5", ">=1.0 <3.0 || dev-master", "1.* || 3.*", "!= dev-foo"}
	versions := []string{"1.0.0.0", "1.0.5.0", "2.3.0.0", "dev-master", "3.1.0.0-beta1"}
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 200 {
				c := mustParse(t, constraints[(g+i)%len(constraints)])
				v := versions[i%len(versions)]
				if CompilingMatcher.Match(c, OpEQ, v) != c.Matches(NewConstraintOp(OpEQ, v)) {
					t.Errorf("%s vs %s: compiled and matches() differ", c, v)
				}
				Intervals.CompactConstraint(newMultiConstraint([]ConstraintInterface{c, mustParse(t, constraints[i%len(constraints)])}, i%2 == 0))
				_ = c.String() + c.LowerBound().String() + c.UpperBound().String()
				if i%50 == 0 {
					CompilingMatcher.Clear()
					Intervals.Clear()
				}
			}
		})
	}
	wg.Wait()
}

func TestMatchesDoesNotAllocate(t *testing.T) {
	require, err := VersionParser{}.ParseConstraints("^1.2.3 || ^2.0, !=2.0.5 || ~3.1.0")
	if err != nil {
		t.Fatal(err)
	}
	provide := NewConstraintOp(OpEQ, "3.1.4.0")
	CompilingMatcher.Match(require, OpEQ, "3.1.4.0")
	for name, f := range map[string]func(){
		"Matches":                func() { require.Matches(provide) },
		"CompilingMatcher.Match": func() { CompilingMatcher.Match(require, OpEQ, "3.1.4.0") },
		"VersionCompare":         func() { VersionCompare("1.10.3.0-beta2", "1.10.3.0-RC1") },
		"ParseStability":         func() { ParseStability("1.10.3.0-beta2") },
	} {
		if allocs := testing.AllocsPerRun(100, f); allocs != 0 {
			t.Errorf("%s allocates %v times", name, allocs)
		}
	}
}
