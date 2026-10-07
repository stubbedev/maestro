// Ports src/Composer/Command/StatusCommand.php.

package command

import (
	"os"
	"strings"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/downloader"
	"github.com/stubbedev/maestro/internal/eventdispatcher"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/pkg/version"
	"github.com/stubbedev/maestro/internal/script"
	"github.com/stubbedev/maestro/internal/util/http"
)

func init() {
	registerCommand(OrderStatus, func() console.Commander { return NewStatusCommand() })
}

// StatusCommand exit codes.
const (
	statusExitCodeErrors          = 1
	statusExitCodeUnpushedChanges = 2
	statusExitCodeVersionChanges  = 4
)

// StatusCommand is Composer\Command\StatusCommand.
type StatusCommand struct{ *BaseCommand }

// NewStatusCommand ports new StatusCommand() (configure()).
func NewStatusCommand() *StatusCommand {
	c := &StatusCommand{BaseCommand: NewBaseCommand("")}
	c.SetImpl(c)
	c.SetName("status")
	c.SetDescription("Shows a list of locally modified packages")
	c.SetDefinitionItems(
		console.MustOption("verbose", "v|vv|vvv", console.OptionValueNone, "Show modified files for each directory that contains changes.", nil),
	)
	c.SetHelp(`The status command displays a list of dependencies that have
been modified locally.

Read more at https://getcomposer.org/doc/03-cli.md#status`)

	return c
}

// PHPClass implements php.Classer.
func (*StatusCommand) PHPClass() string { return `Composer\Command\StatusCommand` }

// Execute ports execute().
func (c *StatusCommand) Execute(in console.Input, out console.Output) (int, error) {
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	commandEvent := eventdispatcher.NewCommandEvent(eventdispatcher.PluginCommand, "status", in, out, nil, nil)
	if _, err := comp.EventDispatcher().Dispatch(commandEvent.Name(), commandEvent); err != nil {
		return 0, err
	}

	// Dispatch pre-status-command
	if _, err := comp.EventDispatcher().DispatchScript(script.PreStatusCmd, true, nil, nil); err != nil {
		return 0, err
	}

	exitCode, err := c.doExecute(in)
	if err != nil {
		return 0, err
	}

	// Dispatch post-status-command
	if _, err := comp.EventDispatcher().DispatchScript(script.PostStatusCmd, true, nil, nil); err != nil {
		return 0, err
	}

	return exitCode, nil
}

type statusVersionChange struct {
	previousVersion, previousRef string
	currentVersion, currentRef   string
}

// indentStatusChanges is the "    " . ltrim($line) indentation of every
// line of a change report.
func indentStatusChanges(changes string) string {
	lines := strings.Split(changes, "\n")
	for i, line := range lines {
		lines[i] = "    " + php.Ltrim(line)
	}

	return strings.Join(lines, "\n")
}

func (c *StatusCommand) doExecute(in console.Input) (int, error) {
	// init repos
	comp, err := c.RequireComposer(nil, nil)
	if err != nil {
		return 0, err
	}

	installedRepo := comp.RepositoryManager().LocalRepository()

	dm := comp.DownloadManager()
	im := comp.InstallationManager()

	// path => changes, in insertion order (PHP arrays)
	errs := php.NewArray()
	out := c.IO()
	unpushedChanges := php.NewArray()
	// path => index into versionChanges
	vcsVersionChanges := php.NewArray()
	var versionChanges []statusVersionChange

	process := comp.Loop().ProcessExecutor()
	if process == nil {
		process = http.NewProcessExecutor(out)
	}
	guesser := version.NewVersionGuesser(version.NewProcessExecutor(process), out)
	var arrayDumper dumper.ArrayDumper

	// list packages
	packages, err := installedRepo.CanonicalPackages()
	if err != nil {
		return 0, err
	}
	for _, p := range packages {
		dl, err := dm.DownloaderForPackage(p)
		if err != nil {
			return 0, err
		}
		targetDir, ok, err := im.InstallPath(p)
		if err != nil {
			return 0, err
		}
		if !ok {
			continue
		}

		if reporter, ok := dl.(downloader.ChangeReporter); ok {
			if fi, err := os.Lstat(targetDir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				errs.Set(targetDir, targetDir+" is a symbolic link.")
			}

			changes, err := reporter.LocalChanges(p, targetDir)
			if err != nil {
				return 0, err
			}
			if changes.Valid {
				errs.Set(targetDir, changes.S)
			}
		}

		if vcs, ok := dl.(downloader.VcsCapableDownloader); ok {
			ref, err := vcs.VcsReference(p, targetDir)
			if err != nil {
				return 0, err
			}
			if ref.Valid && php.ToBool(ref.S) {
				var previousRef string
				switch src := p.InstallationSource(); {
				case src.Is(pkg.FromSource):
					previousRef = p.SourceReference().S
				case src.Is(pkg.FromDist):
					previousRef = p.DistReference().S
				}

				dumped, err := arrayDumper.Dump(p)
				if err != nil {
					return 0, err
				}
				currentVersion, err := guesser.GuessVersion(dumped, targetDir)
				if err != nil {
					return 0, err
				}

				if php.ToBool(previousRef) && currentVersion != nil &&
					(!currentVersion.Commit.Valid || currentVersion.Commit.S != previousRef) &&
					currentVersion.PrettyVersion != previousRef {
					if i, ok := vcsVersionChanges.Get(targetDir); ok {
						idx, _ := i.(int64)
						versionChanges[idx] = statusVersionChange{
							previousVersion: p.PrettyVersion(),
							previousRef:     previousRef,
							currentVersion:  currentVersion.PrettyVersion,
							currentRef:      currentVersion.Commit.S,
						}
					} else {
						vcsVersionChanges.Set(targetDir, int64(len(versionChanges)))
						versionChanges = append(versionChanges, statusVersionChange{
							previousVersion: p.PrettyVersion(),
							previousRef:     previousRef,
							currentVersion:  currentVersion.PrettyVersion,
							currentRef:      currentVersion.Commit.S,
						})
					}
				}
			}
		}

		if dvcs, ok := dl.(downloader.DvcsDownloader); ok {
			unpushed, err := dvcs.UnpushedChanges(p, targetDir)
			if err != nil {
				return 0, err
			}
			if unpushed.Valid && php.ToBool(unpushed.S) {
				unpushedChanges.Set(targetDir, unpushed.S)
			}
		}
	}

	// output errors/warnings
	if errs.Len() == 0 && unpushedChanges.Len() == 0 && vcsVersionChanges.Len() == 0 {
		out.WriteError("<info>No local changes</info>", true, io.Normal)

		return 0, nil
	}

	verbose := console.BoolOption(in, "verbose")

	if errs.Len() > 0 {
		out.WriteError("<error>You have changes in the following dependencies:</error>", true, io.Normal)

		for path, changes := range errs.All() {
			if verbose {
				out.Write("<info>"+path.String()+"</info>:", true, io.Normal)
				out.Write(indentStatusChanges(php.ToString(changes)), true, io.Normal)
			} else {
				out.Write(path.String(), true, io.Normal)
			}
		}
	}

	if unpushedChanges.Len() > 0 {
		out.WriteError("<warning>You have unpushed changes on the current branch in the following dependencies:</warning>", true, io.Normal)

		for path, changes := range unpushedChanges.All() {
			if verbose {
				out.Write("<info>"+path.String()+"</info>:", true, io.Normal)
				out.Write(indentStatusChanges(php.ToString(changes)), true, io.Normal)
			} else {
				out.Write(path.String(), true, io.Normal)
			}
		}
	}

	if vcsVersionChanges.Len() > 0 {
		out.WriteError("<warning>You have version variations in the following dependencies:</warning>", true, io.Normal)

		for path, v := range vcsVersionChanges.All() {
			idx, _ := v.(int64)
			changes := versionChanges[idx]
			if verbose {
				// If we don't can't find a version, use the ref instead.
				currentVersion := changes.currentVersion
				if !php.ToBool(currentVersion) {
					currentVersion = changes.currentRef
				}
				previousVersion := changes.previousVersion
				if !php.ToBool(previousVersion) {
					previousVersion = changes.previousRef
				}

				if out.IsVeryVerbose() {
					// Output the ref regardless of whether or not it's being used as the version
					currentVersion += " (" + changes.currentRef + ")"
					previousVersion += " (" + changes.previousRef + ")"
				}

				out.Write("<info>"+path.String()+"</info>:", true, io.Normal)
				out.Write("    From <comment>"+previousVersion+"</comment> to <comment>"+currentVersion+"</comment>", true, io.Normal)
			} else {
				out.Write(path.String(), true, io.Normal)
			}
		}
	}

	if !verbose {
		out.WriteError("Use --verbose (-v) to see a list of files", true, io.Normal)
	}

	exitCode := 0
	if errs.Len() > 0 {
		exitCode += statusExitCodeErrors
	}
	if unpushedChanges.Len() > 0 {
		exitCode += statusExitCodeUnpushedChanges
	}
	if vcsVersionChanges.Len() > 0 {
		exitCode += statusExitCodeVersionChanges
	}

	return exitCode, nil
}
