// The --format values of the commands that print reports, each on its
// side of the contract (docs/PORTING.md "The contract"): the text and
// table formats are for people and free, colour-only when decorated; the
// others are parsed and frozen.

package command

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/ui"
)

var (
	// jsonTextFormats are show's, outdated's, search's and
	// check-platform-reqs' formats.
	jsonTextFormats = ui.Formats{{Name: "json", Surface: ui.Frozen}, {Name: "text", Surface: ui.Free}}
	// fundFormats are fund's formats.
	fundFormats = ui.Formats{{Name: "text", Surface: ui.Free}, {Name: "json", Surface: ui.Frozen}}
	// licensesFormats are licenses' formats.
	licensesFormats = ui.Formats{{Name: "text", Surface: ui.Free}, {Name: "json", Surface: ui.Frozen}, {Name: "summary", Surface: ui.Frozen}}
)

// formatOption is a command's --format (-f) option, suggesting formats.
func formatOption(description string, def string, formats ui.Formats) *console.InputOption {
	return optionWithSuggestions("format", "f", console.OptionValueRequired, description, def, formats.Names()...)
}
