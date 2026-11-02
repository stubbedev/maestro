package command

import (
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
)

// Test hooks of RequireCommand and Application (RequireCommandTest calls
// updateFile through reflection).

// RequireUpdateFile exposes updateFile.
func RequireUpdateFile(path string, newRequirements *php.Array, requireKey, removeKey string) error {
	file, err := json.NewFile(path, nil, nil)
	if err != nil {
		return err
	}
	c := &RequireCommand{json: file}

	return c.updateFile(newRequirements, requireKey, removeKey, false)
}

// RequireUpdateFileWhole exposes updateFileWhole.
func RequireUpdateFileWhole(path string, newRequirements *php.Array, requireKey, removeKey string) error {
	file, err := json.NewFile(path, nil, nil)
	if err != nil {
		return err
	}
	c := &RequireCommand{json: file}

	return c.updateFileWhole(newRequirements, requireKey, removeKey)
}

// WindowsDriveCacheDirWarning exposes windowsDriveCacheDirWarning.
var WindowsDriveCacheDirWarning = windowsDriveCacheDirWarning
