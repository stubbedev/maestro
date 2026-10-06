// Ports src/Composer/Command/SelfUpdateCommand.php, as maestro's own
// self-update (docs/PORTING.md deviation 4).

package command

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	nethttp "net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

func init() {
	registerCommand(OrderSelfUpdate, func() console.Commander { return NewSelfUpdateCommand() })
}

// The release source of maestro and the names of its assets: every
// GitHub release of github.com/stubbedev/maestro carries one binary per
// platform, maestro_<GOOS>_<GOARCH> (".exe" on Windows), and checksums.txt
// in sha256sum format. Tags are "v" + the version.
const (
	maestroRepository = "stubbedev/maestro"
	githubAPI         = "https://api.github.com"
	checksumsAsset    = "checksums.txt"
	oldInstallSuffix  = "-old"
	channelFile       = "maestro-update-channel"
)

// SelfUpdateCommand is maestro's self-update: Composer's command name,
// aliases, options and messages, updating the maestro binary from the
// GitHub releases of github.com/stubbedev/maestro instead of composer.phar
// from getcomposer.org.
//
// Channels: stable is the latest release, preview and snapshot the latest
// release including pre-releases; --1, --2 and --2.2 (Composer's major
// version channels) have no maestro equivalent and select stable. The
// channel is remembered in <home>/maestro-update-channel. Downloads are
// verified against the release's checksums.txt (there are no signing keys,
// so --update-keys only says so). Backups go to data-dir as
// <date>-<version>-old, which --rollback restores.
type SelfUpdateCommand struct {
	*BaseCommand

	// APIBase is the GitHub API root (tests point it elsewhere).
	APIBase string
	// Executable returns the binary to replace; the running one by
	// default.
	Executable func() (string, error)
	// CurrentVersion is the running maestro's version; the runtime's
	// client version by default.
	CurrentVersion string
}

// NewSelfUpdateCommand ports new SelfUpdateCommand().
func NewSelfUpdateCommand() *SelfUpdateCommand {
	c := &SelfUpdateCommand{BaseCommand: NewBaseCommand(""), APIBase: githubAPI, Executable: currentExecutable}
	c.SetImpl(c)
	c.alwaysDisablePlugins = true
	c.SetName("self-update").
		SetAliases("selfupdate").
		SetDescription("Updates composer.phar to the latest version").
		SetDefinitionItems(
			console.MustOption("rollback", "r", console.OptionValueNone, "Revert to an older installation of composer", nil),
			console.MustOption("clean-backups", "", console.OptionValueNone, "Delete old backups during an update. This makes the current version of composer the only backup available after the update", nil),
			console.MustArgument("version", console.ArgumentOptional, "The version to update to", nil),
			console.MustOption("no-progress", "", console.OptionValueNone, "Do not output download progress.", nil),
			console.MustOption("update-keys", "", console.OptionValueNone, "Prompt user for a key update", nil),
			console.MustOption("stable", "", console.OptionValueNone, "Force an update to the stable channel", nil),
			console.MustOption("preview", "", console.OptionValueNone, "Force an update to the preview channel", nil),
			console.MustOption("snapshot", "", console.OptionValueNone, "Force an update to the snapshot channel", nil),
			console.MustOption("1", "", console.OptionValueNone, "Force an update to the stable channel, but only use 1.x versions", nil),
			console.MustOption("2", "", console.OptionValueNone, "Force an update to the stable channel, but only use 2.x versions", nil),
			console.MustOption("2.2", "", console.OptionValueNone, "Force an update to the stable channel, but only use 2.2.x LTS versions", nil),
			console.MustOption("set-channel-only", "", console.OptionValueNone, "Only store the channel as the default one and then exit", nil),
		).
		SetHelp(`The <info>self-update</info> command checks getcomposer.org for newer
versions of composer and if found, installs the latest.

<info>php composer.phar self-update</info>

Read more at https://getcomposer.org/doc/03-cli.md#self-update-selfupdate`)

	return c
}

// ClassName implements console.ClassNamer.
func (*SelfUpdateCommand) ClassName() string { return `Composer\Command\SelfUpdateCommand` }

// currentExecutable is the running binary with symlinks resolved.
func currentExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}

	return filepath.EvalSymlinks(exe)
}

// assetName is the release asset of this platform.
func assetName() string {
	name := "maestro_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		name += ".exe"
	}

	return name
}

// channels are Versions::CHANNELS.
var channels = []string{"stable", "preview", "snapshot", "1", "2", "2.2"}

// release is the part of a GitHub release maestro uses.
type release struct {
	version string
	assets  map[string]string // name => download URL
}

// Execute implements console.Executor.
func (c *SelfUpdateCommand) Execute(in console.Input, out console.Output) (int, error) {
	current, localFilename, ok := runningMaestro(c.application(), c.CurrentVersion, c.Executable)
	if !ok {
		out.Writeln("<error>This instance of Composer does not have the self-update command.</error>")
		out.Writeln("<comment>This could be due to a number of reasons, such as Composer being installed as a system package on your OS, or Composer being installed as a package in the current project.</comment>")

		return 1, nil
	}

	cfg, err := c.factory().CreateConfig(nil, "")
	if err != nil {
		return 0, err
	}
	get := func(key string) string {
		v, _ := cfg.Get(key, 0)

		return php.ToString(v)
	}

	ioi := c.IO()
	httpDownloader, err := c.factory().CreateHttpDownloader(ioi, cfg, nil)
	if err != nil {
		return 0, err
	}

	home := get("home")

	// switch channel if requested
	requestedChannel := ""
	for _, channel := range channels {
		if console.BoolOption(in, channel) {
			requestedChannel = channel
			if err := setChannel(home, channel, ioi); err != nil {
				return 0, err
			}

			break
		}
	}

	if console.BoolOption(in, "set-channel-only") {
		return 0, nil
	}

	cacheDir := get("cache-dir")
	rollbackDir := get("data-dir")

	if console.BoolOption(in, "update-keys") {
		ioi.WriteError("<warning>maestro verifies its updates with the checksums published with each GitHub release, there are no public keys to update.</warning>", true, io.Normal)

		return 0, nil
	}

	// ensure the maestro binary location is accessible
	if !fileExists(localFilename) {
		return 0, NewError(`Composer\Downloader\FilesystemException`, `Composer update failed: the "`+localFilename+`" is not accessible`)
	}

	// check if current dir is writable and if not try the cache dir from settings
	tmpDir := cacheDir
	if util.IsWritable(filepath.Dir(localFilename)) {
		tmpDir = filepath.Dir(localFilename)
	}

	// check for permissions in local filesystem before start connection process
	if !util.IsWritable(tmpDir) {
		return 0, NewError(`Composer\Downloader\FilesystemException`, `Composer update failed: the "`+tmpDir+`" directory used to download the temp file could not be written`)
	}

	if console.BoolOption(in, "rollback") {
		return c.rollback(rollbackDir, localFilename)
	}

	if in.Argument("command") == "self" && in.Argument("version") == "update" {
		in.SetArgument("version", nil)
	}

	channel := readChannel(home)
	if requestedChannel != "" {
		channel = requestedChannel
	}

	requested := console.StringArgument(in, "version")
	var target *release
	if requested != "" {
		requested = strings.TrimPrefix(requested, "v")
		target, err = releaseSource(c.APIBase).fetch(httpDownloader, "/releases/tags/v"+requested)
		if err != nil {
			if te, ok := errors.AsType[*util.TransportError](err); ok && te.StatusCode == nethttp.StatusNotFound {
				return 0, &Error{Class: ClassInvalidArgument, Message: `Version "` + requested + `" could not be found.`, Prev: err}
			}

			return 0, err
		}
	} else if target, err = releaseSource(c.APIBase).latest(httpDownloader, channel); err != nil {
		return 0, err
	}
	updateVersion := target.version

	if requested == "" && channel == "stable" && semver.VersionCompare(updateVersion, current) < 0 {
		updateVersion = current
	}

	channelString := channel
	if php.IsNumeric(channelString) {
		channelString += ".x"
	}

	if updateVersion == current {
		ioi.WriteError("<info>You are already using the latest available Composer version "+updateVersion+" ("+channelString+" channel).</info>", true, io.Normal)

		// remove all backups except for the most recent, if any
		if console.BoolOption(in, "clean-backups") {
			cleanBackups(rollbackDir, lastBackupVersion(rollbackDir))
		}

		return 0, nil
	}

	asset, ok := target.assets[assetName()]
	if !ok {
		return 0, NewError(ClassRuntime, `Version "`+updateVersion+`" has no build for `+runtime.GOOS+"/"+runtime.GOARCH+".")
	}

	tempFilename := tmpDir + "/" + filepath.Base(localFilename) + "-temp" + strconv.Itoa(int(time.Now().UnixNano()%10000000))
	backupFile := rollbackDir + "/" + backupStamp(localFilename) + "-" + current + oldInstallSuffix

	ioi.Write("Upgrading to version <info>"+updateVersion+"</info> ("+channelString+" channel).", true, io.Normal)

	var checksums string
	if sumURL, ok := target.assets[checksumsAsset]; ok {
		resp, err := httpDownloader.Get(sumURL, nil)
		if err != nil {
			return 0, err
		}
		checksums = resp.Body()
	}
	ioi.WriteError("   ", false, io.Normal)
	if _, err := httpDownloader.Copy(asset, tempFilename, nil); err != nil {
		return 0, err
	}
	ioi.WriteError("", true, io.Normal)

	if !fileExists(tempFilename) || checksums == "" {
		_ = os.Remove(tempFilename)
		ioi.WriteError("<error>The download of the new composer version failed for an unexpected reason</error>", true, io.Normal)

		return 1, nil
	}

	if err := verifyChecksum(tempFilename, checksums, assetName()); err != nil {
		_ = os.Remove(tempFilename)

		return 0, err
	}

	// remove saved installations of maestro
	if console.BoolOption(in, "clean-backups") {
		cleanBackups(rollbackDir, "")
	}

	if !c.setLocalBinary(localFilename, tempFilename, backupFile) {
		_ = os.Remove(tempFilename)

		return 1, nil
	}

	if fileExists(backupFile) {
		ioi.WriteError("Use <info>composer self-update --rollback</info> to return to version <comment>"+current+"</comment>", true, io.Normal)
	} else {
		ioi.WriteError("<warning>A backup of the current version could not be written to "+backupFile+", no rollback possible</warning>", true, io.Normal)
	}

	return 0, nil
}

// runningMaestro is the running maestro's version (without build
// metadata) and binary; ok is false when it has no self-update: a
// development build ("dev", or no version) or a binary a package manager
// owns. override and executable are the commands' test hooks; the version
// is the runtime's client version unless override is set.
func runningMaestro(app *Application, override string, executable func() (string, error)) (current, binary string, ok bool) {
	current = override
	if current == "" && app != nil {
		current = app.Runtime().ClientVersion()
	}
	current, _, _ = strings.Cut(current, "+")

	binary, err := executable()
	if err != nil || current == "" || current == "dev" || isSystemPackage(binary) {
		return current, binary, false
	}

	return current, binary, true
}

// isSystemPackage reports binaries a package manager owns (the Nix store),
// which Composer's "installed as a system package" message covers.
func isSystemPackage(path string) bool {
	return strings.HasPrefix(path, "/nix/store/")
}

// backupStamp is strtr(Composer::RELEASE_DATE, ' :', '_-'): maestro uses
// the modification time of the binary being replaced.
func backupStamp(path string) string {
	t := time.Now()
	if st, err := os.Stat(path); err == nil {
		t = st.ModTime()
	}

	return t.UTC().Format("2006-01-02_15-04-05")
}

// setChannel ports Versions::setChannel: it stores the channel.
func setChannel(home, channel string, ioi io.IO) error {
	if err := os.MkdirAll(home, 0o777); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, channelFile), []byte(channel+php.EOL), 0o666); err != nil {
		return err
	}
	if channel != "stable" && channel != "preview" && channel != "snapshot" {
		ioi.WriteError("<warning>maestro has no "+channel+".x channel, the stable channel is used.</warning>", true, io.Normal)
	}

	return nil
}

// readChannel ports Versions::getChannel.
func readChannel(home string) string {
	data, err := os.ReadFile(filepath.Join(home, channelFile))
	if err != nil {
		return "stable"
	}
	channel := strings.TrimSpace(string(data))
	if !slices.Contains(channels, channel) {
		return "stable"
	}

	return channel
}

// releaseSource is the GitHub API root maestro's releases are read from
// (githubAPI; tests point it elsewhere), shared by self-update and
// diagnose's version check.
type releaseSource string

func (r releaseSource) apiURL(path string) string {
	return strings.TrimRight(string(r), "/") + "/repos/" + maestroRepository + path
}

// fetch reads one release from the GitHub API.
func (r releaseSource) fetch(d *http.HttpDownloader, path string) (*release, error) {
	resp, err := d.Get(r.apiURL(path), nil)
	if err != nil {
		return nil, err
	}
	data, err := resp.DecodeJSONArray()
	if err != nil {
		return nil, err
	}

	return parseRelease(data), nil
}

// latest is the newest release of the channel: stable takes
// /releases/latest (no pre-releases), preview and snapshot the newest of
// /releases.
func (r releaseSource) latest(d *http.HttpDownloader, channel string) (*release, error) {
	if channel != "preview" && channel != "snapshot" {
		return r.fetch(d, "/releases/latest")
	}
	resp, err := d.Get(r.apiURL("/releases?per_page=30"), nil)
	if err != nil {
		return nil, err
	}
	list, err := resp.DecodeJSONArray()
	if err != nil {
		return nil, err
	}
	var best *release
	for _, v := range list.All() {
		a, ok := v.(*php.Array)
		if !ok {
			continue
		}
		if draft, _ := a.Get("draft"); draft == true {
			continue
		}
		rel := parseRelease(a)
		if best == nil || semver.VersionCompare(rel.version, best.version) > 0 {
			best = rel
		}
	}
	if best == nil {
		return nil, NewError(ClassUnexpectedValue, "No release found for the "+channel+" channel")
	}

	return best, nil
}

func parseRelease(a *php.Array) *release {
	tag, _ := a.GetString("tag_name")
	r := &release{version: strings.TrimPrefix(tag, "v"), assets: map[string]string{}}
	if assets, ok := a.GetArray("assets"); ok {
		for _, v := range assets.All() {
			if asset, ok := v.(*php.Array); ok {
				name, _ := asset.GetString("name")
				url, _ := asset.GetString("browser_download_url")
				r.assets[name] = url
			}
		}
	}

	return r
}

// verifyChecksum checks file against its line in a sha256sum listing.
func verifyChecksum(file, checksums, name string) error {
	var want string
	for line := range strings.SplitSeq(checksums, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
		}
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if want == "" || hex.EncodeToString(sum[:]) != want {
		return NewError(ClassUnexpectedValue, "The phar signature did not match the file you downloaded, this means your public keys are outdated or that the phar file is corrupt/has been modified")
	}

	return nil
}

// setLocalBinary ports setLocalPhar: it keeps a backup of localFilename
// and moves newFilename into its place.
func (c *SelfUpdateCommand) setLocalBinary(localFilename, newFilename, backupTarget string) bool {
	ioi := c.IO()
	perm := fs.FileMode(0o755)
	if st, err := os.Stat(localFilename); err == nil {
		perm = st.Mode().Perm()
	}
	if err := os.Chmod(newFilename, perm); err != nil {
		ioi.WriteError("<error>"+err.Error()+"</error>", true, io.Normal)

		return false
	}

	// copy current file into backups dir
	if backupTarget != "" {
		if err := os.MkdirAll(filepath.Dir(backupTarget), 0o777); err == nil {
			_, _ = util.Copy(localFilename, backupTarget)
		}
	}

	if err := replaceFile(newFilename, localFilename); err != nil {
		ioi.WriteError("<error>Composer update failed: \""+localFilename+"\" could not be written.", true, io.Normal)
		ioi.WriteError(err.Error()+"</error>", true, io.Normal)

		return false
	}

	return true
}

// replaceFile moves src over dst; a running Windows binary is moved aside
// first.
func replaceFile(src, dst string) error {
	if runtime.GOOS == "windows" {
		old := dst + ".old"
		_ = os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return err
		}
	}

	return os.Rename(src, dst)
}

// rollback ports rollback.
func (c *SelfUpdateCommand) rollback(rollbackDir, localFilename string) (int, error) {
	rollbackVersion := lastBackupVersion(rollbackDir)
	if rollbackVersion == "" {
		return 0, NewError(ClassUnexpectedValue, `Composer rollback failed: no installation to roll back to in "`+rollbackDir+`"`)
	}

	oldFile := rollbackDir + "/" + rollbackVersion + oldInstallSuffix

	if st, err := os.Stat(oldFile); err != nil || !st.Mode().IsRegular() {
		return 0, NewError(`Composer\Downloader\FilesystemException`, `Composer rollback failed: "`+oldFile+`" could not be found`)
	}
	if !util.IsReadable(oldFile) {
		return 0, NewError(`Composer\Downloader\FilesystemException`, `Composer rollback failed: "`+oldFile+`" could not be read`)
	}

	ioi := c.IO()
	ioi.WriteError("Rolling back to version <info>"+parseBackupVersion(rollbackVersion)+"</info>.", true, io.Normal)

	tmp := localFilename + "-rollback"
	if _, err := util.Copy(oldFile, tmp); err != nil {
		return 0, err
	}
	if !c.setLocalBinary(localFilename, tmp, "") {
		_ = os.Remove(tmp)

		return 1, nil
	}
	_ = os.Remove(oldFile)

	return 0, nil
}

// backups are the backup names (without the -old suffix) in rollbackDir,
// sorted.
func backups(rollbackDir string) []string {
	entries, err := os.ReadDir(rollbackDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if n := e.Name(); strings.HasSuffix(n, oldInstallSuffix) && !e.IsDir() {
			names = append(names, strings.TrimSuffix(n, oldInstallSuffix))
		}
	}
	slices.Sort(names)

	return names
}

// lastBackupVersion ports getLastBackupVersion ("" for null).
func lastBackupVersion(rollbackDir string) string {
	names := backups(rollbackDir)
	if len(names) == 0 {
		return ""
	}

	return names[len(names)-1]
}

// cleanBackups ports cleanBackups: it removes every backup but except.
func cleanBackups(rollbackDir, except string) {
	for _, name := range backups(rollbackDir) {
		if name != except {
			_ = os.Remove(rollbackDir + "/" + name + oldInstallSuffix)
		}
	}
}

// parseBackupVersion is parseBackupVersion's version part: the backup name
// without its "<date>-" prefix.
func parseBackupVersion(rollbackVersion string) string {
	if len(rollbackVersion) > 20 && rollbackVersion[19] == '-' && rollbackVersion[4] == '-' && rollbackVersion[10] == '_' {
		return rollbackVersion[20:]
	}

	return rollbackVersion
}
