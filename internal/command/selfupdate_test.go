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
	"testing"

	"github.com/stubbedev/maestro/internal/command"
	"github.com/stubbedev/maestro/internal/command/commandtest"
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
	for in, want := range map[string]string{
		"2024-06-15_10-30-45-2.5.0":     "2.5.0",
		"2024-06-15_10-30-45-2.4.0-RC1": "2.4.0-RC1",
		"2024-06-15_10-30-45-a1b2c3d":   "a1b2c3d",
		"totally-unexpected":            "totally-unexpected",
	} {
		if got := command.ParseBackupVersion(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

// releaseServer serves a fake GitHub API for stubbedev/maestro.
type releaseServer struct {
	*httptest.Server
	binary    string
	checksum  string
	latest    string
	prerelase string
}

func newReleaseServer(t *testing.T, latest, binary string) *releaseServer {
	t.Helper()
	sum := sha256.Sum256([]byte(binary))
	rs := &releaseServer{binary: binary, checksum: hex.EncodeToString(sum[:]), latest: latest}
	asset := "maestro_" + runtime.GOOS + "_" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		asset += ".exe"
	}
	releaseJSON := func(tag string) string {
		return fmt.Sprintf(`{"tag_name": "v%s", "draft": false, "assets": [
			{"name": %q, "browser_download_url": "%s/download/%s/bin"},
			{"name": "checksums.txt", "browser_download_url": "%s/download/%s/checksums.txt"}
		]}`, tag, asset, rs.URL, tag, rs.URL, tag)
	}
	rs.Server = httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, r *nethttp.Request) {
		p := r.URL.Path
		switch {
		case p == "/repos/stubbedev/maestro/releases/latest":
			fmt.Fprint(w, releaseJSON(rs.latest))
		case p == "/repos/stubbedev/maestro/releases":
			fmt.Fprint(w, "["+releaseJSON(rs.latest)+","+releaseJSON(rs.prerelase)+"]")
		case strings.HasPrefix(p, "/repos/stubbedev/maestro/releases/tags/v"):
			tag := strings.TrimPrefix(p, "/repos/stubbedev/maestro/releases/tags/v")
			if tag != rs.latest && tag != rs.prerelase {
				nethttp.NotFound(w, r)

				return
			}
			fmt.Fprint(w, releaseJSON(tag))
		case strings.HasSuffix(p, "/bin"):
			fmt.Fprint(w, rs.binary)
		case strings.HasSuffix(p, "/checksums.txt"):
			fmt.Fprintf(w, "%s  %s\n%s  other\n", rs.checksum, asset, strings.Repeat("0", 64))
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
	if st, err := os.Stat(bin); err != nil || st.Mode().Perm()&0o100 == 0 {
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
	if !strings.Contains(appTester.Display(true), "Rolling back to version 1.0.0.") {
		t.Errorf("display %q", appTester.Display(true))
	}
	if got := readFile(t, bin2); got != "OLD" {
		t.Errorf("binary after rollback = %q", got)
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
