// Ports src/Composer/Command/ValidateCommand.php.

package command

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg/loader"
	"github.com/stubbedev/maestro/internal/util"
)

func init() {
	registerCommand(OrderValidate, func() console.Commander { return NewValidateCommand() })
}

// ValidateCommand is Composer\Command\ValidateCommand.
type ValidateCommand struct{ *BaseCommand }

// NewValidateCommand ports new ValidateCommand() (configure()).
func NewValidateCommand() *ValidateCommand {
	c := &ValidateCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("validate")
	c.SetDescription("Validates a composer.json and composer.lock")
	c.SetDefinitionItems(
		console.MustOption("no-check-all", "", console.OptionValueNone, "Do not validate requires for overly strict/loose constraints", nil),
		console.MustOption("check-lock", "", console.OptionValueNone, "Check if lock file is up to date (even when config.lock is false)", nil),
		console.MustOption("no-check-lock", "", console.OptionValueNone, "Do not check if lock file is up to date", nil),
		console.MustOption("no-check-publish", "", console.OptionValueNone, "Do not check for publish errors", nil),
		console.MustOption("no-check-version", "", console.OptionValueNone, "Do not report a warning if the version field is present", nil),
		console.MustOption("with-dependencies", "A", console.OptionValueNone, "Also validate the composer.json of all installed dependencies", nil),
		console.MustOption("strict", "", console.OptionValueNone, "Return a non-zero exit code for warnings as well as errors", nil),
		console.MustArgument("file", console.ArgumentOptional, "path to composer.json file", nil),
	)
	c.SetHelp(`The validate command validates a given composer.json and composer.lock

Exit codes in case of errors are:
1 validation warning(s), only when --strict is given
2 validation error(s)
3 file unreadable or missing

Read more at https://getcomposer.org/doc/03-cli.md#validate`)

	return c
}

// PHPClass implements php.Classer.
func (*ValidateCommand) PHPClass() string { return `Composer\Command\ValidateCommand` }

// validateExitCode is `count($errors) > 0 ? 2 : (($isStrict &&
// count($warnings) > 0) ? 1 : 0)`.
func validateExitCode(errs, warnings []string, isStrict bool) int {
	switch {
	case len(errs) > 0:
		return 2
	case isStrict && len(warnings) > 0:
		return 1
	}

	return 0
}

// Execute ports execute().
func (c *ValidateCommand) Execute(in console.Input, out console.Output) (int, error) {
	file, ok := in.Argument("file").(string)
	if !ok {
		var err error
		if file, err = composer.GetComposerFile(); err != nil {
			return 0, err
		}
	}
	cio := c.IO()

	if _, err := os.Stat(file); err != nil {
		cio.WriteError("<error>"+file+" not found.</error>", true, io.Normal)

		return 3, nil
	}
	if !util.IsReadable(file) {
		cio.WriteError("<error>"+file+" is not readable.</error>", true, io.Normal)

		return 3, nil
	}

	validator := NewConfigValidator(cio)
	checkAll := loader.CheckAll
	if console.BoolOption(in, "no-check-all") {
		checkAll = 0
	}
	checkPublish := !console.BoolOption(in, "no-check-publish")
	checkLock := !console.BoolOption(in, "no-check-lock")
	checkVersion := ConfigValidatorCheckVersion
	if console.BoolOption(in, "no-check-version") {
		checkVersion = 0
	}
	isStrict := console.BoolOption(in, "strict")
	errs, publishErrors, warnings, err := validator.Validate(file, checkAll, checkVersion)
	if err != nil {
		return 0, err
	}

	var lockErrors []string
	comp, err := c.CreateComposerInstance(in, cio, file, false, false)
	if err != nil {
		return 0, err
	}
	// config.lock = false ~= implicit --no-check-lock; --check-lock overrides
	lockConfig, err := comp.Config().Get("lock", 0)
	if err != nil {
		return 0, err
	}
	checkLock = (checkLock && php.ToBool(lockConfig)) || console.BoolOption(in, "check-lock")
	locker := comp.Locker()
	locked, err := locker.IsLocked()
	if err != nil {
		return 0, err
	}
	if locked {
		fresh, err := locker.IsFresh()
		if err != nil {
			return 0, err
		}
		if !fresh {
			lockErrors = append(lockErrors, "- The lock file is not up to date with the latest changes in composer.json, it is recommended that you run `composer update` or `composer update <package name>`.")
		}
	}

	if locked {
		info, err := locker.MissingRequirementInfo(comp.Package(), true)
		if err != nil {
			return 0, err
		}
		lockErrors = append(lockErrors, info...)
	}

	errs, warnings = c.outputResult(cio, file, errs, warnings, checkPublish, publishErrors, checkLock, lockErrors, true)

	// $errors include publish and lock errors when exists
	exitCode := validateExitCode(errs, warnings, isStrict)

	if console.BoolOption(in, "with-dependencies") {
		localRepo := comp.RepositoryManager().LocalRepository()
		packages, err := localRepo.Packages()
		if err != nil {
			return 0, err
		}
		for _, p := range packages {
			path, ok, err := comp.InstallationManager().InstallPath(p)
			if err != nil {
				return 0, err
			}
			if !ok {
				continue
			}
			file := path + "/composer.json"
			if fi, err := os.Stat(path); err == nil && fi.IsDir() {
				if _, err := os.Stat(file); err == nil {
					errs, publishErrors, warnings, err := validator.Validate(file, checkAll, checkVersion)
					if err != nil {
						return 0, err
					}

					errs, warnings = c.outputResult(cio, p.PrettyName(), errs, warnings, checkPublish, publishErrors, false, nil, false)

					// $errors include publish errors when exists
					exitCode = max(validateExitCode(errs, warnings, isStrict), exitCode)
				}
			}
		}
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "validate", in, out, nil, nil)
	eventCode, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent)
	if err != nil {
		return 0, err
	}

	return max(eventCode, exitCode), nil
}

func prefixLines(prefix string, list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = prefix + s
	}

	return out
}

// outputResult ports outputResult; it returns $errors and $warnings as
// the method leaves them (they are passed by reference).
func (c *ValidateCommand) outputResult(cio io.IO, name string, errs, warnings []string, checkPublish bool, publishErrors []string, checkLock bool, lockErrors []string, printSchemaURL bool) ([]string, []string) {
	doPrintSchemaURL := false

	switch {
	case len(errs) > 0:
		cio.WriteError("<error>"+name+" is invalid, the following errors/warnings were found:</error>", true, io.Normal)
	case len(publishErrors) > 0 && checkPublish:
		cio.WriteError("<info>"+name+" is valid for simple usage with Composer but has</info>", true, io.Normal)
		cio.WriteError("<info>strict errors that make it unable to be published as a package</info>", true, io.Normal)
		doPrintSchemaURL = printSchemaURL
	case len(warnings) > 0:
		cio.WriteError("<info>"+name+" is valid, but with a few warnings</info>", true, io.Normal)
		doPrintSchemaURL = printSchemaURL
	case len(lockErrors) > 0:
		kind := "warnings"
		if checkLock {
			kind = "errors"
		}
		cio.Write("<info>"+name+" is valid but your composer.lock has some "+kind+"</info>", true, io.Normal)
	default:
		cio.Write("<info>"+name+" is valid</info>", true, io.Normal)
	}

	if doPrintSchemaURL {
		cio.WriteError("<warning>See https://getcomposer.org/doc/04-schema.md for details on the schema</warning>", true, io.Normal)
	}

	if len(errs) > 0 {
		errs = append([]string{"# General errors"}, prefixLines("- ", errs)...)
	}
	if len(warnings) > 0 {
		warnings = append([]string{"# General warnings"}, prefixLines("- ", warnings)...)
	}

	// Avoid setting the exit code to 1 in case --strict and --no-check-publish/--no-check-lock are combined
	var extraWarnings []string

	// If checking publish errors, display them as errors, otherwise just show them as warnings
	if len(publishErrors) > 0 && checkPublish {
		publishErrors = append([]string{"# Publish errors"}, prefixLines("- ", publishErrors)...)
		errs = append(errs, publishErrors...)
	}

	// If checking lock errors, display them as errors, otherwise just show them as warnings
	if len(lockErrors) > 0 {
		if checkLock {
			errs = append(append(errs, "# Lock file errors"), lockErrors...)
		} else {
			extraWarnings = append(append(extraWarnings, "# Lock file warnings"), lockErrors...)
		}
	}

	messages := []struct {
		style string
		msgs  []string
	}{
		{"error", errs},
		{"warning", append(append([]string{}, warnings...), extraWarnings...)},
	}

	for _, m := range messages {
		for _, msg := range m.msgs {
			if strings.HasPrefix(msg, "#") {
				cio.WriteError("<"+m.style+">"+msg+"</"+m.style+">", true, io.Normal)
			} else {
				cio.WriteError(msg, true, io.Normal)
			}
		}
	}

	return errs, warnings
}
