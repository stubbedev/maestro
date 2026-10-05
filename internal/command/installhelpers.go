// Helpers shared by the install-type commands of group A (install,
// update, reinstall, dump-autoload, require, remove, create-project):
// small adaptations of internal/composer's interfaces to what the PHP
// commands call.

package command

import (
	"github.com/stubbedev/maestro/internal/config"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
)

// optionWithSuggestions is `new InputOption(..., $suggestedValues)` for a
// static list of suggestions.
func optionWithSuggestions(name, shortcut string, mode int, description string, def any, values ...string) *console.InputOption {
	o, err := console.MustOption(name, shortcut, mode, description, def).WithSuggestedValues(values...)
	if err != nil {
		panic(err)
	}

	return o
}

// optionWithSuggestFunc is `new InputOption(..., $suggestedValues)` for a
// completion callback; a nil fn leaves the option without one.
func optionWithSuggestFunc(name, shortcut string, mode int, description string, def any, fn console.SuggestFunc) *console.InputOption { //nolint:unparam // new InputOption's parameter list; group B shares it
	o, err := console.MustOption(name, shortcut, mode, description, def).WithSuggestFunc(fn)
	if err != nil {
		panic(err)
	}

	return o
}

// outputProgressSetter is InstallationManager::setOutputProgress, which
// composer.InstallationManager leaves out (*installer.Manager has it).
type outputProgressSetter interface {
	SetOutputProgress(outputProgress bool)
}

// setOutputProgress calls setOutputProgress on the installation manager
// when it has it (a substitute in tests may not).
func setOutputProgress(im any, outputProgress bool) {
	if s, ok := im.(outputProgressSetter); ok {
		s.SetOutputProgress(outputProgress)
	}
}

// configTruthy is `(bool) $config->get($key)` as used in a PHP condition.
func configTruthy(cfg *config.Config, key string) (bool, error) {
	v, err := cfg.Get(key, 0)
	if err != nil {
		return false, err
	}

	return php.ToBool(v), nil
}

// apcuOptions computes the commands' $apcuPrefix and $apcu:
//
//	$apcuPrefix = $input->getOption($prefixOption);
//	$apcu = $apcuPrefix !== null || $input->getOption($apcuOption) || $config->get('apcu-autoloader');
func apcuOptions(in console.Input, cfg *config.Config, apcuOption, prefixOption string) (apcu bool, prefix *string, err error) {
	if v := in.Option(prefixOption); v != nil {
		s := php.ToString(v)
		prefix = &s
	}
	if prefix != nil || console.BoolOption(in, apcuOption) {
		return true, prefix, nil
	}
	apcu, err = configTruthy(cfg, "apcu-autoloader")

	return apcu, prefix, err
}

// optionOrConfig is `$input->getOption($option) || $config->get($key)`.
func optionOrConfig(in console.Input, option string, cfg *config.Config, key string) (bool, error) {
	if console.BoolOption(in, option) {
		return true, nil
	}

	return configTruthy(cfg, key)
}
