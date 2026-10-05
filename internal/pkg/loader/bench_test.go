package loader_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

// BenchmarkLoadPackages loads every version of the p2 samples as
// ComposerRepository does; it reports packages per second.
func BenchmarkLoadPackages(b *testing.B) {
	t := &testing.T{}
	inputs := p2Inputs(t)

	var (
		all   [][]*php.Array
		count int
	)

	for _, versions := range inputs {
		all = append(all, versions)
		count += len(versions)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		parser := pkg.NewVersionParser()

		for _, versions := range all {
			if _, err := loader.NewArrayLoader(parser, false).LoadPackages(versions); err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ReportMetric(float64(count)*float64(b.N)/b.Elapsed().Seconds(), "packages/s")
}
