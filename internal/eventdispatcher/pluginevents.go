// Ports src/Composer/Plugin/PluginEvents.php, CommandEvent.php,
// PreCommandRunEvent.php, PreFileDownloadEvent.php,
// PostFileDownloadEvent.php and PrePoolCreateEvent.php.
//
// These plain data events live here rather than in internal/plugin because
// the packages dispatching them (internal/command, internal/downloader,
// internal/repository, internal/resolver) sit below internal/plugin and all
// import this package. The deprecated PostFileDownloadEvent::getPackage and
// its constructor's Composer 2.0 calling convention exist only for PHP
// plugins and are left to the shim.

package eventdispatcher

import (
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util/http"
)

// The PluginEvents constants.
const (
	// PluginInit is dispatched after the plugins are loaded (INIT).
	PluginInit = "init"
	// PluginCommand is dispatched before a command runs (COMMAND).
	PluginCommand = "command"
	// PreFileDownload is dispatched before a file is downloaded.
	PreFileDownload = "pre-file-download"
	// PostFileDownload is dispatched after a file is downloaded.
	PostFileDownload = "post-file-download"
	// PreCommandRun is dispatched before a command is executed, allowing
	// its input to be modified.
	PreCommandRun = "pre-command-run"
	// PrePoolCreate is dispatched before the pool is created, allowing its
	// packages to be filtered.
	PrePoolCreate = "pre-pool-create"
)

// CommandEvent is Composer\Plugin\CommandEvent.
type CommandEvent struct {
	BaseEvent
	commandName string
	input       console.Input
	output      console.Output
}

// NewCommandEvent is new CommandEvent($name, $commandName, $input,
// $output, $args, $flags).
func NewCommandEvent(name, commandName string, input console.Input, output console.Output, args []string, flags *php.Array) *CommandEvent {
	e := &CommandEvent{commandName: commandName, input: input, output: output}
	e.init(name, args, flags)

	return e
}

// Class implements Event.
func (*CommandEvent) Class() string { return `Composer\Plugin\CommandEvent` }

// Input is getInput().
func (e *CommandEvent) Input() console.Input { return e.input }

// Output is getOutput().
func (e *CommandEvent) Output() console.Output { return e.output }

// CommandName is getCommandName().
func (e *CommandEvent) CommandName() string { return e.commandName }

// PreCommandRunEvent is Composer\Plugin\PreCommandRunEvent.
type PreCommandRunEvent struct {
	BaseEvent
	input   console.Input
	command string
}

// NewPreCommandRunEvent is new PreCommandRunEvent($name, $input, $command).
func NewPreCommandRunEvent(name string, input console.Input, command string) *PreCommandRunEvent {
	e := &PreCommandRunEvent{input: input, command: command}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*PreCommandRunEvent) Class() string { return `Composer\Plugin\PreCommandRunEvent` }

// Input is getInput().
func (e *PreCommandRunEvent) Input() console.Input { return e.input }

// Command is getCommand(): the name of the command being run.
func (e *PreCommandRunEvent) Command() string { return e.command }

// PreFileDownloadEvent is Composer\Plugin\PreFileDownloadEvent.
type PreFileDownloadEvent struct {
	BaseEvent
	httpDownloader   http.Getter
	processedURL     string
	customCacheKey   pkg.NullString
	typ              string
	context          any
	transportOptions *php.Array
}

// NewPreFileDownloadEvent is new PreFileDownloadEvent($name,
// $httpDownloader, $processedUrl, $type, $context). typ is "package" or
// "metadata"; context is the package, or the metadata's array/null.
func NewPreFileDownloadEvent(name string, httpDownloader http.Getter, processedURL, typ string, context any) *PreFileDownloadEvent {
	e := &PreFileDownloadEvent{httpDownloader: httpDownloader, processedURL: processedURL, typ: typ, context: context, transportOptions: php.NewArray()}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*PreFileDownloadEvent) Class() string { return `Composer\Plugin\PreFileDownloadEvent` }

// HttpDownloader is getHttpDownloader(); in production a
// *http.HttpDownloader.
func (e *PreFileDownloadEvent) HttpDownloader() http.Getter { return e.httpDownloader }

// ProcessedURL is getProcessedUrl().
func (e *PreFileDownloadEvent) ProcessedURL() string { return e.processedURL }

// SetProcessedURL is setProcessedUrl().
func (e *PreFileDownloadEvent) SetProcessedURL(processedURL string) {
	e.processedURL = processedURL
	e.bump()
}

// CustomCacheKey is getCustomCacheKey().
func (e *PreFileDownloadEvent) CustomCacheKey() pkg.NullString { return e.customCacheKey }

// SetCustomCacheKey is setCustomCacheKey().
func (e *PreFileDownloadEvent) SetCustomCacheKey(customCacheKey pkg.NullString) {
	e.customCacheKey = customCacheKey
	e.bump()
}

// Type is getType(): "package" or "metadata".
func (e *PreFileDownloadEvent) Type() string { return e.typ }

// Context is getContext().
func (e *PreFileDownloadEvent) Context() any { return e.context }

// TransportOptions is getTransportOptions().
func (e *PreFileDownloadEvent) TransportOptions() *php.Array { return e.transportOptions }

// SetTransportOptions is setTransportOptions().
func (e *PreFileDownloadEvent) SetTransportOptions(options *php.Array) {
	e.transportOptions = options
	e.bump()
}

// PostFileDownloadEvent is Composer\Plugin\PostFileDownloadEvent.
type PostFileDownloadEvent struct {
	BaseEvent
	fileName pkg.NullString
	checksum pkg.NullString
	url      string
	context  any
	typ      string
}

// NewPostFileDownloadEvent is new PostFileDownloadEvent($name, $fileName,
// $checksum, $url, $type, $context).
func NewPostFileDownloadEvent(name string, fileName, checksum pkg.NullString, url, typ string, context any) *PostFileDownloadEvent {
	e := &PostFileDownloadEvent{fileName: fileName, checksum: checksum, url: url, context: context, typ: typ}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*PostFileDownloadEvent) Class() string { return `Composer\Plugin\PostFileDownloadEvent` }

// FileName is getFileName(): the downloaded file, null for metadata.
func (e *PostFileDownloadEvent) FileName() pkg.NullString { return e.fileName }

// Checksum is getChecksum().
func (e *PostFileDownloadEvent) Checksum() pkg.NullString { return e.checksum }

// URL is getUrl().
func (e *PostFileDownloadEvent) URL() string { return e.url }

// Context is getContext().
func (e *PostFileDownloadEvent) Context() any { return e.context }

// Type is getType().
func (e *PostFileDownloadEvent) Type() string { return e.typ }

// PrePoolCreateEvent is Composer\Plugin\PrePoolCreateEvent. The request is
// an internal/resolver Request; the stability and alias data are the PHP
// arrays PoolBuilder holds.
type PrePoolCreateEvent struct {
	BaseEvent
	repositories              []pkg.Repository
	request                   any
	acceptableStabilities     *php.Array
	stabilityFlags            *php.Array
	rootAliases               *php.Array
	rootReferences            *php.Array
	packages                  []pkg.PackageInterface
	unacceptableFixedPackages []pkg.PackageInterface
}

// NewPrePoolCreateEvent is new PrePoolCreateEvent($name, $repositories,
// $request, $acceptableStabilities, $stabilityFlags, $rootAliases,
// $rootReferences, $packages, $unacceptableFixedPackages).
func NewPrePoolCreateEvent(name string, repositories []pkg.Repository, request any, acceptableStabilities, stabilityFlags, rootAliases, rootReferences *php.Array, packages, unacceptableFixedPackages []pkg.PackageInterface) *PrePoolCreateEvent {
	e := &PrePoolCreateEvent{
		repositories:              repositories,
		request:                   request,
		acceptableStabilities:     acceptableStabilities,
		stabilityFlags:            stabilityFlags,
		rootAliases:               rootAliases,
		rootReferences:            rootReferences,
		packages:                  packages,
		unacceptableFixedPackages: unacceptableFixedPackages,
	}
	e.init(name, nil, nil)

	return e
}

// Class implements Event.
func (*PrePoolCreateEvent) Class() string { return `Composer\Plugin\PrePoolCreateEvent` }

// Repositories is getRepositories().
func (e *PrePoolCreateEvent) Repositories() []pkg.Repository { return e.repositories }

// Request is getRequest().
func (e *PrePoolCreateEvent) Request() any { return e.request }

// AcceptableStabilities is getAcceptableStabilities().
func (e *PrePoolCreateEvent) AcceptableStabilities() *php.Array { return e.acceptableStabilities }

// StabilityFlags is getStabilityFlags().
func (e *PrePoolCreateEvent) StabilityFlags() *php.Array { return e.stabilityFlags }

// RootAliases is getRootAliases().
func (e *PrePoolCreateEvent) RootAliases() *php.Array { return e.rootAliases }

// RootReferences is getRootReferences().
func (e *PrePoolCreateEvent) RootReferences() *php.Array { return e.rootReferences }

// Packages is getPackages().
func (e *PrePoolCreateEvent) Packages() []pkg.PackageInterface { return e.packages }

// UnacceptableFixedPackages is getUnacceptableFixedPackages().
func (e *PrePoolCreateEvent) UnacceptableFixedPackages() []pkg.PackageInterface {
	return e.unacceptableFixedPackages
}

// SetPackages is setPackages().
func (e *PrePoolCreateEvent) SetPackages(packages []pkg.PackageInterface) {
	e.packages = packages
	e.bump()
}

// SetUnacceptableFixedPackages is setUnacceptableFixedPackages().
func (e *PrePoolCreateEvent) SetUnacceptableFixedPackages(packages []pkg.PackageInterface) {
	e.unacceptableFixedPackages = packages
	e.bump()
}
