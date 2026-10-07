package command

import (
	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/ui"
)

// OutputFormats are the formats of every command's --format and
// --audit-format option, by command and option.
var OutputFormats = map[string]map[string]ui.Formats{
	"audit":               {"format": advisory.Formats},
	"check-platform-reqs": {"format": jsonTextFormats},
	"fund":                {"format": fundFormats},
	"licenses":            {"format": licensesFormats},
	"outdated":            {"format": jsonTextFormats},
	"search":              {"format": jsonTextFormats},
	"show":                {"format": jsonTextFormats},
	"install":             {"audit-format": advisory.Formats},
	"update":              {"audit-format": advisory.Formats},
	"require":             {"audit-format": advisory.Formats},
	"remove":              {"audit-format": advisory.Formats},
	"create-project":      {"audit-format": advisory.Formats},
}
