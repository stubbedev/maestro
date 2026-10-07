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
	"os/user"
	"path/filepath"
	"regexp"
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
// <date>-<version>-old, which --rollback restores once the backup matches
// the checksums.txt of that version's release.
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

	return php.EvalSymlinks(exe)
}

// assetName is the release asset of this platform.
func assetName() string {
	name := "maestro_" + runtime.GOOS + "_" + runtime.GOARCH
	if php.IsWindows() {
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
	if !php.FileExists(localFilename) {
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
		return c.rollback(httpDownloader, rollbackDir, localFilename, tmpDir)
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
		_ = os.Remove(tempFilename)

		return 0, err
	}
	ioi.WriteError("", true, io.Normal)

	if !php.FileExists(tempFilename) || checksums == "" {
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

	if err := c.setLocalBinary(localFilename, tempFilename, backupFile); err != nil {
		return 0, err
	}

	if php.FileExists(backupFile) {
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
			want = php.Strtolower(fields[0])
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
// in backupTarget (none when "", a rollback) and moves newFilename into
// its place. newFilename is gone afterwards unless it took that place.
func (c *SelfUpdateCommand) setLocalBinary(localFilename, newFilename, backupTarget string) error {
	perm := fs.FileMode(0o755)
	if st, err := os.Stat(localFilename); err == nil {
		perm = st.Mode().Perm()
	}
	action := "Composer update"
	if backupTarget == "" {
		action = "Composer rollback"
	}
	fail := func(err error) error {
		_ = os.Remove(newFilename)

		return NewError(`Composer\Downloader\FilesystemException`, action+` failed: "`+localFilename+`" could not be written.`+php.EOL+err.Error())
	}
	if err := os.Chmod(newFilename, perm); err != nil {
		return fail(err)
	}

	// copy current file into backups dir; a partial copy is no backup
	if backupTarget != "" {
		if err := os.MkdirAll(filepath.Dir(backupTarget), 0o777); err == nil {
			if ok, err := util.Copy(localFilename, backupTarget); !ok || err != nil {
				_ = os.Remove(backupTarget)
			}
		}
	}

	if err := replaceFile(newFilename, localFilename); err != nil {
		return fail(err)
	}

	return nil
}

// replaceFile moves src over dst. Windows can not replace a running
// binary but can rename it, so there dst is moved aside to dst.old first
// and moved back when src can not take its place; the .old file of a
// binary still running is removed by the next update.
func replaceFile(src, dst string) error {
	if !php.IsWindows() {
		return os.Rename(src, dst)
	}
	old := dst + ".old"
	_ = os.Remove(old)
	if err := os.Rename(dst, old); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		_ = os.Rename(old, dst)

		return err
	}
	_ = os.Remove(old)

	return nil
}

// rollback ports rollback. The backup is verified like a download before
// it replaces the binary: a backup of a tagged release against the sha256
// that release publishes in checksums.txt (a mismatch, or a release or
// checksums file that can not be fetched, refuses the rollback); a backup
// of anything else, which no release covers, is restored after a warning
// and, interactively, a confirmation. The backup is copied first and the
// copy is what is verified and installed, so it can not change in
// between.
func (c *SelfUpdateCommand) rollback(d *http.HttpDownloader, rollbackDir, localFilename, tmpDir string) (int, error) {
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
	ioi.WriteError("Rolling back to version <info>"+rollbackVersion+"</info>.", true, io.Normal)

	// The backup about to be installed over the binary must be trustworthy. If its directory or
	// the file itself is owned by another user or writable by others, it may have been tampered
	// with, so warn and ask for confirmation before trusting it.
	untrusted := warnIfUntrustedPath(ioi, rollbackDir, "data-dir")
	untrusted = warnIfUntrustedPath(ioi, oldFile, "backup file") || untrusted
	if untrusted {
		if ok, err := confirmRollback(ioi, "Do you want to roll back to this backup despite the warning above? [<comment>y/N</comment>] "); err != nil || !ok {
			return 1, err
		}
	}

	tmpFile, err := os.CreateTemp(tmpDir, filepath.Base(localFilename)+"-rollback*")
	if err != nil {
		return 0, err
	}
	tmp := tmpFile.Name()
	_ = tmpFile.Close()
	defer func() { _ = os.Remove(tmp) }()
	if ok, err := util.Copy(oldFile, tmp); err != nil {
		return 0, err
	} else if !ok {
		return 0, NewError(`Composer\Downloader\FilesystemException`, `Composer rollback failed: "`+oldFile+`" could not be read`)
	}

	version, isTag := parseBackupVersion(rollbackVersion)
	if !isTag {
		// Snapshot/dev builds are not released so no checksum is published for them.
		ioi.WriteError(`<warning>The signature of "`+rollbackVersion+`" can not be verified as no signature is published for snapshot/dev builds. Make sure your data-dir ("`+rollbackDir+`") is not writable by untrusted users.</warning>`, true, io.Normal)
		if ok, err := confirmRollback(ioi, "Do you want to roll back to this unverified backup anyway? [<comment>y/N</comment>] "); err != nil || !ok {
			return 1, err
		}
	} else if err := c.verifyBackup(d, version, tmp); err != nil {
		// refused before setLocalBinary installs the backup
		return 0, err
	}

	if err := c.setLocalBinary(localFilename, tmp, ""); err != nil {
		return 0, err
	}
	_ = os.Remove(oldFile)

	return 0, nil
}

// confirmRollback asks question when interactive (yes otherwise) and
// says the rollback is aborted on no.
func confirmRollback(ioi io.IO, question string) (bool, error) {
	if !ioi.IsInteractive() {
		return true, nil
	}
	ok, err := ioi.AskConfirmation(question, false)
	if err != nil {
		return false, err
	}
	if !ok {
		ioi.WriteError("<warning>Rollback aborted.</warning>", true, io.Normal)
	}

	return ok, nil
}

// verifyBackup checks file, a backup of release version, against the
// checksums.txt of that release, as a download of it is checked.
func (c *SelfUpdateCommand) verifyBackup(d *http.HttpDownloader, version, file string) error {
	const unverified = ", aborting to avoid installing an unverified maestro binary."
	rel, err := releaseSource(c.APIBase).fetch(d, "/releases/tags/v"+version)
	if err != nil {
		if te, ok := errors.AsType[*util.TransportError](err); ok && te.StatusCode == nethttp.StatusNotFound {
			return &Error{Class: ClassRuntime, Message: `Composer rollback failed: no release "v` + version + `" is published to verify the backup against` + unverified, Prev: err}
		}

		return &Error{Class: ClassRuntime, Message: "Composer rollback failed: could not download the release v" + version + " to verify the backup" + unverified + " Retry once you are online.", Prev: err}
	}
	sumURL, ok := rel.assets[checksumsAsset]
	if !ok {
		return NewError(ClassRuntime, `Composer rollback failed: the release "v`+version+`" publishes no `+checksumsAsset+` to verify the backup against`+unverified)
	}
	resp, err := d.Get(sumURL, nil)
	if err != nil {
		return &Error{Class: ClassRuntime, Message: "Composer rollback failed: could not download the checksums from " + sumURL + " to verify the backup" + unverified + " Retry once you are online.", Prev: err}
	}
	if strings.TrimSpace(resp.Body()) == "" {
		return NewError(ClassRuntime, "Composer rollback failed: an empty checksums file was downloaded from "+sumURL)
	}

	return verifyChecksum(file, resp.Body(), assetName())
}

// warnIfUntrustedPath ports warnIfUntrustedDir: it warns when path is
// owned by another user than the one running maestro or is writable by
// group or others, and reports whether it did. Like Composer without the
// POSIX functions, it checks nothing on Windows.
func warnIfUntrustedPath(ioi io.IO, path, label string) bool {
	if php.IsWindows() {
		return false
	}
	st, err := os.Stat(path)
	if err != nil {
		return false
	}

	untrusted := false
	if uid, ok := fileOwner(st); ok && uid != os.Geteuid() {
		me, err1 := user.LookupId(strconv.Itoa(os.Geteuid()))
		owner, err2 := user.LookupId(strconv.Itoa(uid))
		if err1 == nil && err2 == nil && me.Username != owner.Username {
			ioi.WriteError(`<warning>You are running Composer as "`+me.Username+`", while "`+path+`" (`+label+`) is owned by "`+owner.Username+`"</warning>`, true, io.Normal)
			untrusted = true
		}
	}

	// group- or world-writable paths let other users tamper with files that maestro trusts there
	if st.Mode().Perm()&0o022 != 0 {
		ioi.WriteError(`<warning>The `+label+` "`+path+`" is writable by other users, which is a security risk as another user could tamper with the files Composer trusts there. Make sure it is only writable by the user running Composer.</warning>`, true, io.Normal)
		untrusted = true
	}

	return untrusted
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

// backupNameRe and snapshotRe are parseBackupVersion's patterns.
var (
	backupNameRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}-(.+)$`)
	snapshotRe   = regexp.MustCompile(`^[0-9a-f]{7}$`)
)

// parseBackupVersion ports parseBackupVersion: the version a backup name
// holds (the name without its "<date>-" prefix) and whether it is a
// tagged release; names it does not recognise are returned whole,
// untagged.
func parseBackupVersion(rollbackVersion string) (version string, isTag bool) {
	m := backupNameRe.FindStringSubmatch(rollbackVersion)
	if m == nil {
		return rollbackVersion, false
	}

	return m[1], !snapshotRe.MatchString(m[1])
}
