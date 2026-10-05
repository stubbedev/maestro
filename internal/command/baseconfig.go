// Ports src/Composer/Command/BaseConfigCommand.php.

package command

import (
	"os"

	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

const baseConfigCommandFile = "BaseConfigCommand.php"

// BaseConfigCommand is Composer\Command\BaseConfigCommand, the base of
// config, repository and policy. Its commands define --global and --file.
type BaseConfigCommand struct {
	*BaseCommand

	// Config, ConfigFile and ConfigSource are $config, $configFile and
	// $configSource, set by Initialize.
	Config       *config.Config
	ConfigFile   *json.File
	ConfigSource *config.JSONConfigSource
}

// NewBaseConfigCommand returns a BaseConfigCommand named name.
func NewBaseConfigCommand(name string) *BaseConfigCommand {
	return &BaseConfigCommand{BaseCommand: NewBaseCommand(name)}
}

// Initialize ports initialize.
func (c *BaseConfigCommand) Initialize(in console.Input, out console.Output) error {
	if err := c.BaseCommand.Initialize(in, out); err != nil {
		return err
	}

	global := console.BoolOption(in, "global")
	if global && in.Option("file") != nil {
		return NewError(ClassRuntime, baseConfigCommandFile, 46, "--file and --global can not be combined")
	}

	io := c.IO()
	factory := c.factory()
	cfg, err := factory.CreateConfig(io, "")
	if err != nil {
		return err
	}
	c.Config = cfg

	// When using --global flag, set baseDir to home directory for correct absolute path resolution
	if global {
		home, err := cfg.Get("home", 0)
		if err != nil {
			return err
		}
		cfg.SetBaseDir(php.ToString(home))
	}

	configFile, err := c.ComposerConfigFile(in, cfg)
	if err != nil {
		return err
	}

	// Create global composer.json if invoked using `composer global [config-cmd]`
	if configFile == "composer.json" || configFile == "./composer.json" {
		if !fileExists(configFile) {
			home, err := cfg.Get("home", 0)
			if err != nil {
				return err
			}
			cwd, err := util.GetCwd(false)
			if err != nil {
				return err
			}
			a, aok := util.RealpathOK(cwd)
			b, bok := util.RealpathOK(php.ToString(home))
			if aok == bok && a == b {
				if err := os.WriteFile(configFile, []byte("{\n}\n"), 0o666); err != nil {
					return &util.ErrorException{Message: "file_put_contents(" + configFile + "): Failed to open stream: " + err.Error()}
				}
			}
		}
	}

	if c.ConfigFile, err = json.NewFile(configFile, nil, io); err != nil {
		return err
	}
	c.ConfigSource = config.NewJSONConfigSource(c.ConfigFile, false)

	// Initialize the global file if it's not there, ignoring any warnings or notices
	if global && !c.ConfigFile.Exists() {
		path := c.ConfigFile.Path()
		if f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666); err == nil { //nolint:gosec // touch() creates the file with the default mode
			_ = f.Close()
		}
		if err := c.ConfigFile.Write(php.ArrayOf("config", php.NewObject()), json.DefaultEncodeFlags); err != nil {
			return err
		}
		_ = os.Chmod(path, 0o600)
	}

	if !c.ConfigFile.Exists() {
		return NewError(ClassRuntime, baseConfigCommandFile, 79, `File "`+configFile+`" cannot be found in the current directory`)
	}

	return nil
}

// factory is the Factory of the Application (Factory:: statics in PHP).
func (c *BaseCommand) factory() *composerFactory {
	if app := c.application(); app != nil {
		return app.Factory()
	}

	return &composerFactory{}
}

// ComposerConfigFile ports getComposerConfigFile: the local composer.json,
// the global config.json, or the file passed with --file.
func (*BaseConfigCommand) ComposerConfigFile(in console.Input, cfg *config.Config) (string, error) {
	if console.BoolOption(in, "global") {
		home, err := cfg.Get("home", 0)
		if err != nil {
			return "", err
		}

		return php.ToString(home) + "/config.json", nil
	}
	if f := in.Option("file"); f != nil {
		return php.ToString(f), nil
	}

	return composerFile()
}

// AuthConfigFile ports getAuthConfigFile.
func (c *BaseConfigCommand) AuthConfigFile(in console.Input, cfg *config.Config) (string, error) {
	if console.BoolOption(in, "global") {
		home, err := cfg.Get("home", 0)
		if err != nil {
			return "", err
		}

		return php.ToString(home) + "/auth.json", nil
	}
	file, err := c.ComposerConfigFile(in, cfg)
	if err != nil {
		return "", err
	}

	return util.Dirname(file) + "/auth.json", nil
}
