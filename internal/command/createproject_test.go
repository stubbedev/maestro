package command_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
)

// TestCreateProjectCommand_InputErrors runs create-project's option
// checks, which fail before any repository is read.
func TestCreateProjectCommand_InputErrors(t *testing.T) {
	createProject := func(kv ...any) []any { return append([]any{"package", "x/y", "directory", "out"}, kv...) }
	runCommandCases(t, func(t *testing.T) { commandtest.InitTempDir(t) }, []commandCase{
		{
			name:   "an unknown --prefer-install",
			params: cmd("create-project", createProject("--prefer-install", "bogus")...),
			err:    `--prefer-install accepts one of "dist", "source" or "auto", got bogus`,
		},
		{
			name:   "--prefer-source with --prefer-install",
			params: cmd("create-project", createProject("--prefer-source", true, "--prefer-install", "dist")...),
			err:    "--prefer-source can not be used together with --prefer-install",
		},
		{
			name:   "--prefer-dist with --prefer-install",
			params: cmd("create-project", createProject("--prefer-dist", true, "--prefer-install", "source")...),
			err:    "--prefer-dist can not be used together with --prefer-install",
		},
	})
}
