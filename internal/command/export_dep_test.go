package command

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/repository"
)

// Test hooks of BaseDependencyCommand and PackageDiscovery.

// InitStylesForTest is initStyles.
func (c *BaseDependencyCommand) InitStylesForTest(out console.Output) { c.initStyles(out) }

// PrintTreeForTest is printTree($results).
func (c *BaseDependencyCommand) PrintTreeForTest(results []repository.Dependent) error {
	return c.printTree(results, "", 1)
}

// PrintTableForTest is printTable.
func (c *BaseDependencyCommand) PrintTableForTest(out console.Output, results []repository.Dependent) error {
	return c.printTable(out, results)
}

// MinimumStabilityForTest is getMinimumStability.
func (d *PackageDiscovery) MinimumStabilityForTest(in console.Input) (string, error) {
	return d.minimumStability(in)
}

// FindSimilarForTest is findSimilar.
func (d *PackageDiscovery) FindSimilarForTest(name string) ([]string, error) {
	return d.findSimilar(name)
}

// PHPVersionForSelector exposes phpVersionForSelector.
var PHPVersionForSelector = phpVersionForSelector
