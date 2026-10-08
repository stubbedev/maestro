// Ports tests/Composer/Test/Command/SelfUpdateCommandTest.php where it
// applies to maestro's self-update (the rest of that test runs a built
// composer.phar against getcomposer.org), plus tests of maestro's GitHub
// release flow against a local server.

package command_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

func TestSelfUpdateCommand_UpdateWithInvalidOptionThrowsException(t *testing.T) {
	appTester := commandtest.GetApplicationTester(t)
	_, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "invalid-option", true)
	if err == nil || err.Error() != `The "invalid-option" argument does not exist.` || !isPHPInstance(err, "InvalidArgumentException") {
		t.Fatalf("got %v (%s)", err, phpClassOf(err))
	}
}

func TestSelfUpdateCommand_ParseBackupVersion(t *testing.T) {
	type parsed struct {
		version string
		isTag   bool
	}
	for in, want := range map[string]parsed{
		"2024-06-15_10-30-45-2.5.0":     {"2.5.0", true},
		"2024-06-15_10-30-45-2.4.0-RC1": {"2.4.0-RC1", true},
		"2024-06-15_10-30-45-a1b2c3d":   {"a1b2c3d", false},
		"totally-unexpected":            {"totally-unexpected", false},
	} {
		if version, isTag := command.ParseBackupVersion(in); version != want.version || isTag != want.isTag {
			t.Errorf("%s: got %q %v, want %v", in, version, isTag, want)
		}
	}
}

// releaseServer serves a fake GitHub API for stubbedev/maestro: the
// releases latest and prerelase (binary, listed in checksums.txt as
// checksum) and the older releases in old (tag => binary, listed with its
// real sha256). Releases in noChecksums publish no checksums.txt, those
// in noAsset no build for this platform; answers replace the answer to a
// path. requests counts the requests served.
type releaseServer struct {
	*httptest.Server
	binary      string
	checksum    string
	latest      string
	prerelase   string
	old         map[string]string
	noChecksums map[string]bool
	noAsset     map[string]bool
	answers     map[string]nethttp.HandlerFunc
	requests    atomic.Int32
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))

	return hex.EncodeToString(sum[:])
}

// releaseAsset is the name of this platform's build in a release.
func releaseAsset() string {
	asset := "maestro_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}

	return asset
}

func newReleaseServer(t *testing.T, latest, binary string) *releaseServer {
	t.Helper()
	rs := &releaseServer{
		binary: binary, checksum: sha256Hex(binary), latest: latest, old: map[string]string{},
		noChecksums: map[string]bool{}, noAsset: map[string]bool{}, answers: map[string]nethttp.HandlerFunc{},
	}
	asset := releaseAsset()
	releaseJSON := func(tag string) string {
		var assets []string
		if !rs.noAsset[tag] {
			assets = append(assets, fmt.Sprintf(`{"name": %q, "browser_download_url": "%s/download/%s/bin"}`, asset, rs.URL, tag))
		}
		if !rs.noChecksums[tag] {
			assets = append(assets, fmt.Sprintf(`{"name": "checksums.txt", "browser_download_url": "%s/download/%s/checksums.txt"}`, rs.URL, tag))
		}

		return fmt.Sprintf(`{"tag_name": "v%s", "draft": false, "assets": [%s]}`, tag, strings.Join(assets, ","))
	}
	tagOf := func(p string) string {
		return strings.Split(strings.TrimPrefix(p, "/download/"), "/")[0]
	}
	rs.Server = httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		rs.requests.Add(1)
		p := r.URL.Path
		if answer, ok := rs.answers[p]; ok {
			answer(w, r)

			return
		}
		switch {
		case p == "/repos/stubbedev/maestro/releases/latest":
			fmt.Fprint(w, releaseJSON(rs.latest))
		case p == "/repos/stubbedev/maestro/releases":
			fmt.Fprint(w, "["+releaseJSON(rs.latest)+","+releaseJSON(rs.prerelase)+"]")
		case strings.HasPrefix(p, "/repos/stubbedev/maestro/releases/tags/v"):
			tag := strings.TrimPrefix(p, "/repos/stubbedev/maestro/releases/tags/v")
			if _, ok := rs.old[tag]; !ok && tag != rs.latest && tag != rs.prerelase {
				nethttp.NotFound(w, r)

				return
			}
			fmt.Fprint(w, releaseJSON(tag))
		case strings.HasSuffix(p, "/bin"):
			if bin, ok := rs.old[tagOf(p)]; ok {
				fmt.Fprint(w, bin)
			} else {
				fmt.Fprint(w, rs.binary)
			}
		case strings.HasSuffix(p, "/checksums.txt"):
			sum := rs.checksum
			if bin, ok := rs.old[tagOf(p)]; ok {
				sum = sha256Hex(bin)
			}
			fmt.Fprintf(w, "%s  %s\n%s  other\n", sum, asset, strings.Repeat("0", 64))
		default:
			nethttp.NotFound(w, r)
		}
	}))
	t.Cleanup(rs.Close)

	return rs
}

// selfUpdateTester prepares an application whose self-update replaces a
// temporary "binary" of version current, talking to rs.
func selfUpdateTester(t *testing.T, rs *releaseServer, current string) (*commandtest.ApplicationTester, string, string) {
	t.Helper()
	dir := commandtest.InitTempComposer(t, nil, nil, nil, true)
	home, _ := util.GetEnv("COMPOSER_HOME")
	if err := os.MkdirAll(home, 0o777); err != nil {
		t.Fatal(err)
	}
	// the local server speaks http
	if err := os.WriteFile(home+"/config.json", []byte(`{"config": {"secure-http": false}}`), 0o666); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin", "maestro")
	if err := os.MkdirAll(filepath.Dir(bin), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}

	appTester := commandtest.GetApplicationTester(t)
	cmd, err := appTester.Application.Find("self-update")
	if err != nil {
		t.Fatal(err)
	}
	su := cmd.(*command.SelfUpdateCommand)
	su.APIBase = rs.URL
	su.Executable = func() (string, error) { return bin, nil }
	su.CurrentVersion = current

	return appTester, bin, home
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func TestSelfUpdateCommand_SuccessfulUpdateAndRollback(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.old["1.0.0"] = "OLD"
	appTester, bin, home := selfUpdateTester(t, rs, "1.0.0+abc")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update")
	if err != nil || code != 0 {
		t.Fatalf("self-update: %d %v\n%s", code, err, appTester.Display(true))
	}
	display := appTester.Display(true)
	if !strings.Contains(display, "Upgrading to version 1.2.0 (stable channel).") {
		t.Errorf("display %q", display)
	}
	if !strings.Contains(display, "Use composer self-update --rollback to return to version 1.0.0") {
		t.Errorf("display %q", display)
	}
	if got := readFile(t, bin); got != "NEW" {
		t.Errorf("binary = %q", got)
	}
	// Windows keeps no execute bit.
	if st, err := os.Stat(bin); err != nil || !util.IsWindows() && st.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary mode: %v %v", st, err)
	}

	// --rollback restores the backup
	appTester, bin2, _ := selfUpdateTester(t, rs, "1.2.0")
	if err := os.WriteFile(bin2, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	// the second temp project has its own COMPOSER_HOME: move the backup there
	home2, _ := util.GetEnv("COMPOSER_HOME")
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), "-1.0.0-old") {
			if err := os.Rename(filepath.Join(home, e.Name()), filepath.Join(home2, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	code, err = appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--rollback", true)
	if err != nil || code != 0 {
		t.Fatalf("rollback: %d %v\n%s", code, err, appTester.Display(true))
	}
	if !strings.Contains(appTester.Display(true), "-1.0.0.") || !strings.Contains(appTester.Display(true), "Rolling back to version ") {
		t.Errorf("display %q", appTester.Display(true))
	}
	if got := readFile(t, bin2); got != "OLD" {
		t.Errorf("binary after rollback = %q", got)
	}
}

// TestSelfUpdateCommand_ChecksumsDownloadAlongsideTheBuild: the checksums
// are requested together with the build, not before it.
func TestSelfUpdateCommand_ChecksumsDownloadAlongsideTheBuild(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	buildRequested := make(chan struct{})
	var together atomic.Bool
	rs.answers["/download/1.2.0/bin"] = func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		close(buildRequested)
		fmt.Fprint(w, rs.binary)
	}
	rs.answers["/download/1.2.0/checksums.txt"] = func(w nethttp.ResponseWriter, _ *nethttp.Request) {
		select {
		case <-buildRequested:
			together.Store(true)
		case <-time.After(5 * time.Second):
		}
		fmt.Fprintf(w, "%s  %s\n", rs.checksum, releaseAsset())
	}
	appTester, bin, _ := selfUpdateTester(t, rs, "1.0.0+abc")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update")
	if err != nil || code != 0 {
		t.Fatalf("self-update: %d %v\n%s", code, err, appTester.Display(true))
	}
	if !together.Load() {
		t.Error("the build was requested only once the checksums came")
	}
	if got := readFile(t, bin); got != "NEW" {
		t.Errorf("binary = %q", got)
	}
}

func TestSelfUpdateCommand_AlreadyLatest(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	appTester, bin, _ := selfUpdateTester(t, rs, "1.2.0")

	if code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update"); err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	if !strings.Contains(appTester.Display(true), "You are already using the latest available Composer version 1.2.0 (stable channel).") {
		t.Errorf("display %q", appTester.Display(true))
	}
	if got := readFile(t, bin); got != "OLD" {
		t.Errorf("binary = %q", got)
	}
}

func TestSelfUpdateCommand_PreviewChannel(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.prerelase = "1.3.0-RC1"
	appTester, _, _ := selfUpdateTester(t, rs, "1.0.0")

	if code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--preview", true); err != nil || code != 0 {
		t.Fatalf("%d %v\n%s", code, err, appTester.Display(true))
	}
	if !strings.Contains(appTester.Display(true), "Upgrading to version 1.3.0-RC1 (preview channel).") {
		t.Errorf("display %q", appTester.Display(true))
	}
}

func TestSelfUpdateCommand_ChecksumMismatch(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.checksum = strings.Repeat("a", 64)
	appTester, bin, _ := selfUpdateTester(t, rs, "1.0.0")

	_, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update")
	if err == nil || !strings.Contains(err.Error(), "The phar signature did not match") {
		t.Fatalf("got %v", err)
	}
	if got := readFile(t, bin); got != "OLD" {
		t.Errorf("binary = %q", got)
	}
}

func TestSelfUpdateCommand_UnknownVersion(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	appTester, _, _ := selfUpdateTester(t, rs, "1.0.0")

	_, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "version", "9.9.9")
	if err == nil || err.Error() != `Version "9.9.9" could not be found.` || !isPHPInstance(err, "InvalidArgumentException") {
		t.Fatalf("got %v", err)
	}
	if phperr.PreviousOf(err) == nil {
		t.Errorf("the 404 is not the previous exception of %v", err)
	}

	// The 404 is only the previous exception: the InvalidArgumentException
	// is no TransportException, whose code would take over the exit code.
	appTester.Application.SetCatchExceptions(true)
	if code, _ := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "version", "9.9.9"); code != 1 {
		t.Errorf("exit code %d, want 1\n%s", code, appTester.Display(true))
	}
}

func TestSelfUpdateCommand_DevBuildHasNoSelfUpdate(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	appTester, _, _ := selfUpdateTester(t, rs, "dev")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update")
	if err != nil || code != 1 {
		t.Fatalf("%d %v", code, err)
	}
	want := "This instance of Composer does not have the self-update command.\n" +
		"This could be due to a number of reasons, such as Composer being installed as a system package on your OS, or Composer being installed as a package in the current project.\n"
	if appTester.Display(true) != want {
		t.Errorf("display %q", appTester.Display(true))
	}
}

// plantBackup writes a backup named name (without -old) holding content
// into the data-dir of the current temp project.
func plantBackup(t *testing.T, home, name, content string) string {
	t.Helper()
	path := filepath.Join(home, name+"-old")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

// assertNoRollbackLeftovers fails when a rollback temp file stayed next
// to bin.
func assertNoRollbackLeftovers(t *testing.T, bin string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Dir(bin))
	for _, e := range entries {
		if strings.Contains(e.Name(), "-rollback") {
			t.Errorf("leftover %s", e.Name())
		}
	}
}

func TestSelfUpdateCommand_RollbackRefusesTamperedTaggedBackup(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.old["1.0.0"] = "OLD"
	appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
	// a backup named as the 1.0.0 release whose contents are not 1.0.0's
	backup := plantBackup(t, home, "2024-01-01_00-00-00-1.0.0", "EVIL")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--rollback", true, "--no-interaction", true)
	if err == nil || !strings.Contains(err.Error(), "The phar signature did not match") {
		t.Fatalf("got %d %v\n%s", code, err, appTester.Display(true))
	}
	if got := readFile(t, bin); got != "OLD" {
		t.Errorf("binary = %q", got)
	}
	if got := readFile(t, backup); got != "EVIL" {
		t.Errorf("backup = %q", got)
	}
	assertNoRollbackLeftovers(t, bin)
}

func TestSelfUpdateCommand_RollbackRestoresVerifiedBackup(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.old["1.0.0"] = "GENUINE"
	appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
	backup := plantBackup(t, home, "2024-01-01_00-00-00-1.0.0", "GENUINE")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--rollback", true, "--no-interaction", true)
	if err != nil || code != 0 {
		t.Fatalf("got %d %v\n%s", code, err, appTester.Display(true))
	}
	// (the local server's http warning follows)
	if want := "Rolling back to version 2024-01-01_00-00-00-1.0.0.\n"; !strings.HasPrefix(appTester.Display(true), want) {
		t.Errorf("display %q, want %q", appTester.Display(true), want)
	}
	if got := readFile(t, bin); got != "GENUINE" {
		t.Errorf("binary = %q", got)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Errorf("backup kept: %v", err)
	}
	assertNoRollbackLeftovers(t, bin)
}

func TestSelfUpdateCommand_RollbackWithoutPublishedChecksum(t *testing.T) {
	for name, setup := range map[string]struct {
		prepare func(rs *releaseServer)
		want    string
	}{
		"no checksums.txt": {func(rs *releaseServer) {
			rs.old["1.0.0"] = "OLD"
			rs.noChecksums["1.0.0"] = true
		}, `the release "v1.0.0" publishes no checksums.txt to verify the backup against`},
		"no release": {func(*releaseServer) {}, `no release "v1.0.0" is published to verify the backup against`},
	} {
		t.Run(name, func(t *testing.T) {
			rs := newReleaseServer(t, "1.2.0", "NEW")
			setup.prepare(rs)
			appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
			plantBackup(t, home, "2024-01-01_00-00-00-1.0.0", "OLD")

			_, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--rollback", true, "--no-interaction", true)
			if err == nil || !strings.Contains(err.Error(), "Composer rollback failed: "+setup.want) || !isPHPInstance(err, "RuntimeException") {
				t.Fatalf("got %v", err)
			}
			if got := readFile(t, bin); got != "OLD" {
				t.Errorf("binary = %q", got)
			}
			assertNoRollbackLeftovers(t, bin)
		})
	}
}

func TestSelfUpdateCommand_RollbackToSnapshotBackupWarnsButProceeds(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
	plantBackup(t, home, "2024-01-01_00-00-00-abc1234", "SNAPSHOT")

	code, err := appTester.RunArgs(commandtest.Options{}, "command", "self-update", "--rollback", true, "--no-interaction", true)
	if err != nil || code != 0 {
		t.Fatalf("got %d %v\n%s", code, err, appTester.Display(true))
	}
	if !strings.Contains(appTester.Display(true), "no signature is published for snapshot/dev builds") {
		t.Errorf("display %q", appTester.Display(true))
	}
	if got := readFile(t, bin); got != "SNAPSHOT" {
		t.Errorf("binary = %q", got)
	}
}

func TestSelfUpdateCommand_RollbackToSnapshotBackupAsksInteractively(t *testing.T) {
	rs := newReleaseServer(t, "1.2.0", "NEW")
	appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
	backup := plantBackup(t, home, "2024-01-01_00-00-00-abc1234", "SNAPSHOT")
	appTester.SetInputs("n")

	interactive := true
	code, err := appTester.RunArgs(commandtest.Options{Interactive: &interactive}, "command", "self-update", "--rollback", true)
	if err != nil || code != 1 {
		t.Fatalf("got %d %v\n%s", code, err, appTester.Display(true))
	}
	display := appTester.Display(true)
	if !strings.Contains(display, "Do you want to roll back to this unverified backup anyway?") || !strings.Contains(display, "Rollback aborted.") {
		t.Errorf("display %q", display)
	}
	if got := readFile(t, bin); got != "OLD" {
		t.Errorf("binary = %q", got)
	}
	if got := readFile(t, backup); got != "SNAPSHOT" {
		t.Errorf("backup = %q", got)
	}
	assertNoRollbackLeftovers(t, bin)
}

func TestSelfUpdateCommand_RollbackWarnsAboutWritableBackup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Composer checks no permissions on Windows")
	}
	rs := newReleaseServer(t, "1.2.0", "NEW")
	rs.old["1.0.0"] = "GENUINE"
	appTester, bin, home := selfUpdateTester(t, rs, "1.2.0")
	backup := plantBackup(t, home, "2024-01-01_00-00-00-1.0.0", "GENUINE")
	if err := os.Chmod(backup, 0o666); err != nil {
		t.Fatal(err)
	}
	appTester.SetInputs("n")

	interactive := true
	code, err := appTester.RunArgs(commandtest.Options{Interactive: &interactive}, "command", "self-update", "--rollback", true)
	if err != nil || code != 1 {
		t.Fatalf("got %d %v\n%s", code, err, appTester.Display(true))
	}
	display := appTester.Display(true)
	if !strings.Contains(display, `The backup file "`+backup+`" is writable by other users`) || !strings.Contains(display, "Rollback aborted.") {
		t.Errorf("display %q", display)
	}
	if got := readFile(t, bin); got != "OLD" {
		t.Errorf("binary = %q", got)
	}
}

// selfUpdateRun is a self-update run against a release server whose latest
// stable release is 1.2.0: the name it is run by (self-update when
// empty), the running version (1.0.0 when empty), the server's preview
// release, a setup (the server's switches, a stored channel, backups),
// the arguments, and what the run gives: an exception whose message holds
// err, or a status code and a display holding display; the binary
// afterwards ("OLD" is the one replaced, "NEW" the latest release's), the
// stored channel ("" leaves it unchecked), the suffixes of the backups
// left (nil leaves them unchecked), and, with offline, that the server
// was asked nothing.
type selfUpdateRun struct {
	name      string
	command   string
	current   string
	prerelase string
	setup     func(t *testing.T, rs *releaseServer, su *command.SelfUpdateCommand, bin, home string)
	args      []any
	err       string
	code      int
	display   []string
	binary    string
	channel   string
	backups   []string
	offline   bool
}

// writeChannel stores channel as the one self-update remembers.
func writeChannel(t *testing.T, home, channel string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(home, "maestro-update-channel"), []byte(channel+"\n"), 0o666); err != nil {
		t.Fatal(err)
	}
}

// backupNames are the backups in home, without their -old suffix.
func backupNames(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), "-old"); ok {
			names = append(names, name)
		}
	}

	return names
}

func runSelfUpdate(t *testing.T, runs []selfUpdateRun) {
	t.Helper()
	for _, tc := range runs {
		t.Run(tc.name, func(t *testing.T) {
			rs := newReleaseServer(t, "1.2.0", "NEW")
			rs.prerelase = tc.prerelase
			current := tc.current
			if current == "" {
				current = "1.0.0"
			}
			appTester, bin, home := selfUpdateTester(t, rs, current)
			found, err := appTester.Application.Find("self-update")
			if err != nil {
				t.Fatal(err)
			}
			if tc.setup != nil {
				tc.setup(t, rs, found.(*command.SelfUpdateCommand), bin, home)
			}

			name := tc.command
			if name == "" {
				name = "self-update"
			}
			code, err := appTester.RunArgs(commandtest.Options{}, append([]any{"command", name}, tc.args...)...)
			display := appTester.Display(true)
			switch {
			case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
				t.Fatalf("exception %v, want one holding %q (display:\n%s)", err, tc.err, display)
			case tc.err == "" && err != nil:
				t.Fatalf("unexpected exception: %v (display:\n%s)", err, display)
			case tc.err == "" && code != tc.code:
				t.Errorf("status %d, want %d (display:\n%s)", code, tc.code, display)
			}
			for _, s := range tc.display {
				if !strings.Contains(display, s) {
					t.Errorf("display lacks %q:\n%s", s, display)
				}
			}
			if got := readFile(t, bin); got != tc.binary {
				t.Errorf("binary %q, want %q", got, tc.binary)
			}
			if entries, _ := os.ReadDir(filepath.Dir(bin)); len(entries) != 1 {
				t.Errorf("the binary's directory holds %v, want the binary alone", entries)
			}
			if tc.channel != "" {
				if got := readFile(t, filepath.Join(home, "maestro-update-channel")); got != tc.channel+php.EOL {
					t.Errorf("stored channel %q, want %q", got, tc.channel)
				}
			}
			if tc.backups != nil {
				got := backupNames(t, home)
				if len(got) != len(tc.backups) {
					t.Fatalf("backups %v, want %v", got, tc.backups)
				}
				for i, suffix := range tc.backups {
					if !strings.HasSuffix(got[i], suffix) {
						t.Errorf("backup %q, want one ending in %q", got[i], suffix)
					}
				}
			}
			if n := rs.requests.Load(); tc.offline && n != 0 {
				t.Errorf("%d requests, want none", n)
			}
		})
	}
}

// TestSelfUpdateCommand_Runs runs self-update's names, versions, channels
// and options against the fake GitHub API.
func TestSelfUpdateCommand_Runs(t *testing.T) {
	const upgraded = "Upgrading to version 1.2.0 (stable channel)."
	planted := func(names ...string) func(*testing.T, *releaseServer, *command.SelfUpdateCommand, string, string) {
		return func(t *testing.T, _ *releaseServer, _ *command.SelfUpdateCommand, _, home string) {
			for _, name := range names {
				plantBackup(t, home, name, "BACKUP")
			}
		}
	}
	older := func(t *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
		rs.old["1.1.0"] = "V110"
	}
	runs := []selfUpdateRun{
		{name: "the latest stable release", display: []string{upgraded}, binary: "NEW", backups: []string{"-1.0.0"}},
		{name: "the selfupdate alias", command: "selfupdate", display: []string{upgraded}, binary: "NEW"},
		{name: "self update in two words", command: "self", args: []any{"version", "update"}, display: []string{upgraded}, binary: "NEW"},
		{name: "a version", setup: older, args: []any{"version", "1.1.0"}, display: []string{"Upgrading to version 1.1.0 (stable channel)."}, binary: "V110"},
		{name: "a version with its v", setup: older, args: []any{"version", "v1.1.0"}, display: []string{"Upgrading to version 1.1.0 (stable channel)."}, binary: "V110"},
		{name: "an older version", current: "1.2.0", setup: older, args: []any{"version", "1.1.0"}, display: []string{"Upgrading to version 1.1.0 (stable channel)."}, binary: "V110"},
		{
			name:    "a newer build than the latest release",
			current: "1.3.0",
			display: []string{"You are already using the latest available Composer version 1.3.0 (stable channel)."},
			binary:  "OLD",
		},
		{name: "--snapshot", prerelase: "1.3.0-RC1", args: []any{"--snapshot", true}, display: []string{"Upgrading to version 1.3.0-RC1 (snapshot channel)."}, binary: "NEW", channel: "snapshot"},
		{
			name:      "--stable after preview",
			prerelase: "1.3.0-RC1",
			setup: func(t *testing.T, _ *releaseServer, _ *command.SelfUpdateCommand, _, home string) {
				writeChannel(t, home, "preview")
			},
			args:    []any{"--stable", true},
			display: []string{upgraded},
			binary:  "NEW",
			channel: "stable",
		},
		{
			name:      "the stored channel",
			prerelase: "1.3.0-RC1",
			setup: func(t *testing.T, _ *releaseServer, _ *command.SelfUpdateCommand, _, home string) {
				writeChannel(t, home, "preview")
			},
			display: []string{"Upgrading to version 1.3.0-RC1 (preview channel)."},
			binary:  "NEW",
			channel: "preview",
		},
		{name: "--set-channel-only", args: []any{"--set-channel-only", true, "--preview", true}, display: []string{`Storing "preview" as default update channel for the next self-update run.`}, binary: "OLD", channel: "preview", offline: true},
		{
			name:    "--update-keys",
			args:    []any{"--update-keys", true},
			display: []string{"there are no public keys to update"},
			binary:  "OLD",
			offline: true,
		},
		{name: "--no-progress", args: []any{"--no-progress", true}, display: []string{upgraded}, binary: "NEW"},
		{
			name:    "--clean-backups on update",
			setup:   planted("2024-01-01_00-00-00-0.8.0", "2024-02-01_00-00-00-0.9.0"),
			args:    []any{"--clean-backups", true},
			display: []string{upgraded},
			binary:  "NEW",
			backups: []string{"-1.0.0"},
		},
		{
			name:    "--clean-backups when up to date",
			current: "1.2.0",
			setup:   planted("2024-01-01_00-00-00-0.8.0", "2024-02-01_00-00-00-0.9.0"),
			args:    []any{"--clean-backups", true},
			binary:  "OLD",
			backups: []string{"-0.9.0"},
		},
	}
	// A major channel is stored as stable, an LTS one ("2.2") as itself,
	// as Versions::setChannel does.
	for major, stored := range map[string]string{"1": "stable", "2": "stable", "2.2": "2.2"} {
		runs = append(runs, selfUpdateRun{
			name: "--" + major,
			args: []any{"--" + major, true},
			display: []string{
				`Storing "` + stored + `" as default update channel for the next self-update run.`,
				"maestro has no " + major + ".x channel, the stable channel is used.",
				"Upgrading to version 1.2.0 (" + major + ".x channel).",
			},
			binary:  "NEW",
			channel: stored,
		})
	}
	runSelfUpdate(t, runs)
}

// TestSelfUpdateCommand_Failures runs self-update where the release, the
// GitHub API or the binary's location fails it: the binary stays as it
// was and no temporary file is left next to it.
func TestSelfUpdateCommand_Failures(t *testing.T) {
	answer := func(status int, body string) nethttp.HandlerFunc {
		return func(w nethttp.ResponseWriter, _ *nethttp.Request) {
			w.WriteHeader(status)
			fmt.Fprint(w, body)
		}
	}
	latest := "/repos/stubbedev/maestro/releases/latest"
	runs := []selfUpdateRun{
		{name: "--rollback without backups", args: []any{"--rollback", true}, err: "Composer rollback failed: no installation to roll back to in ", binary: "OLD"},
		{
			name: "no build for this platform",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.noAsset["1.2.0"] = true
			},
			err:    `Version "1.2.0" has no build for ` + runtime.GOOS + "/" + runtime.GOARCH + ".",
			binary: "OLD",
		},
		{
			name: "no checksums.txt",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.noChecksums["1.2.0"] = true
			},
			code:    1,
			display: []string{"The download of the new composer version failed for an unexpected reason"},
			binary:  "OLD",
		},
		{
			name: "no checksum for this platform",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.answers["/download/1.2.0/checksums.txt"] = answer(nethttp.StatusOK, sha256Hex("NEW")+"  other\n")
			},
			err:    "The phar signature did not match",
			binary: "OLD",
		},
		{
			name: "the API answers 500",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.answers[latest] = answer(500, "oops")
			},
			err:    "500",
			binary: "OLD",
		},
		{
			name: "the API answers no JSON",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.answers[latest] = answer(200, "{nope")
			},
			err:    "JSON",
			binary: "OLD",
		},
		{
			name:   "the API refuses the connection",
			setup:  func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) { rs.Close() },
			err:    "Failed to connect",
			binary: "OLD",
		},
		{
			name: "no preview release",
			setup: func(_ *testing.T, rs *releaseServer, _ *command.SelfUpdateCommand, _, _ string) {
				rs.answers["/repos/stubbedev/maestro/releases"] = answer(200, `[{"tag_name": "v1.3.0", "draft": true, "assets": []}]`)
			},
			args:   []any{"--preview", true},
			err:    "No release found for the preview channel",
			binary: "OLD",
		},
		{
			name: "a binary under /nix/store",
			setup: func(_ *testing.T, _ *releaseServer, su *command.SelfUpdateCommand, _, _ string) {
				su.Executable = func() (string, error) { return "/nix/store/abc-maestro/bin/maestro", nil }
			},
			code:    1,
			display: []string{"This instance of Composer does not have the self-update command."},
			binary:  "OLD",
			offline: true,
		},
		{
			name: "a missing binary",
			setup: func(_ *testing.T, _ *releaseServer, su *command.SelfUpdateCommand, bin, _ string) {
				su.Executable = func() (string, error) { return bin + "-gone", nil }
			},
			err:    `Composer update failed: the "`,
			binary: "OLD",
		},
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		runs = append(runs, selfUpdateRun{
			name: "no writable directory for the download",
			setup: func(t *testing.T, _ *releaseServer, _ *command.SelfUpdateCommand, bin, _ string) {
				cache := t.TempDir()
				t.Setenv("COMPOSER_CACHE_DIR", cache)
				for _, dir := range []string{cache, filepath.Dir(bin)} {
					if err := os.Chmod(dir, 0o555); err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
				}
			},
			err:    "directory used to download the temp file could not be written",
			binary: "OLD",
		})
	}
	runSelfUpdate(t, runs)
}
