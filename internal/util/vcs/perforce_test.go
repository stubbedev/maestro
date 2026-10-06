// Ports tests/Composer/Test/Util/PerforceTest.php.

package vcs

import (
	"bufio"
	"errors"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

const (
	testDepot      = "depot"
	testBranch     = "branch"
	testP4User     = "user"
	testClientName = "TEST"
	testPort       = "port"
	testPath       = "path"
)

type perforceFixture struct {
	perforce   *Perforce
	process    *processmock.Mock
	repoConfig *php.Array
	io         *fakeIO
}

func getTestRepoConfig() *php.Array {
	return php.ArrayOf("depot", testDepot, "branch", testBranch, "p4user", testP4User, "unique_perforce_client_name", testClientName)
}

// newPerforceFixture is setUp(): it works in a temporary directory, as
// the Perforce constructor creates its (relative) path.
func newPerforceFixture(t *testing.T) *perforceFixture {
	t.Helper()
	t.Chdir(t.TempDir())

	f := &perforceFixture{process: processmock.New(), repoConfig: getTestRepoConfig(), io: newFakeIO()}
	f.createNewPerforceWithWindowsFlag(t, true)

	return f
}

func (f *perforceFixture) createNewPerforceWithWindowsFlag(t *testing.T, flag bool) {
	t.Helper()

	p, err := newPerforce(f.repoConfig, testPort, testPath, f.process, flag, f.io, "p4")
	if err != nil {
		t.Fatal(err)
	}

	f.perforce = p
}

func (f *perforceFixture) setPerforceToStream() { f.perforce.SetStream("//depot/branch") }

func expectEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()

	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestPerforce_GetClientWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	expectEqual(t, f.perforce.GetClient(), "composer_perforce_TEST_depot")
}

func TestPerforce_GetClientFromStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	expectEqual(t, f.perforce.GetClient(), "composer_perforce_TEST_depot_branch")
}

func TestPerforce_GetStreamWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	expectEqual(t, f.perforce.GetStream(), "//depot")
}

func TestPerforce_GetStreamWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	expectEqual(t, f.perforce.GetStream(), "//depot/branch")
}

func TestPerforce_GetStreamWithoutLabelWithStreamWithoutLabel(t *testing.T) {
	f := newPerforceFixture(t)
	expectEqual(t, f.perforce.GetStreamWithoutLabel("//depot/branch"), "//depot/branch")
}

func TestPerforce_GetStreamWithoutLabelWithStreamWithLabel(t *testing.T) {
	f := newPerforceFixture(t)
	expectEqual(t, f.perforce.GetStreamWithoutLabel("//depot/branching@label"), "//depot/branching")
}

func TestPerforce_GetClientSpec(t *testing.T) {
	f := newPerforceFixture(t)
	expectEqual(t, f.perforce.GetP4ClientSpec(), "path/composer_perforce_TEST_depot.p4.spec")
}

func TestPerforce_GenerateP4Command(t *testing.T) {
	f := newPerforceFixture(t)
	got := f.perforce.GenerateP4Command([]string{"do", "something"}, true)
	expectEqual(t, strings.Join(got, " "), "p4 -u user -c composer_perforce_TEST_depot -p port do something")
}

func TestPerforce_QueryP4UserWithUserAlreadySet(t *testing.T) {
	f := newPerforceFixture(t)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, strOf(f.perforce.GetUser()), testP4User)
}

func TestPerforce_QueryP4UserWithUserSetInP4VariablesWithWindowsOS(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, true)
	f.perforce.SetUser(nil)
	f.process.Expects([]processmock.Expectation{{Cmd: util.ShellCmd("p4 set"), Stdout: "P4USER=TEST_P4VARIABLE_USER" + php.EOL}}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, strOf(f.perforce.GetUser()), "TEST_P4VARIABLE_USER")
}

func TestPerforce_QueryP4UserWithUserSetInP4VariablesNotWindowsOS(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, false)
	f.perforce.SetUser(nil)
	f.process.Expects([]processmock.Expectation{{Cmd: util.ShellCmd("echo $P4USER"), Stdout: "TEST_P4VARIABLE_USER" + php.EOL}}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, strOf(f.perforce.GetUser()), "TEST_P4VARIABLE_USER")
}

func askAnswering(t *testing.T, fio *fakeIO, question, answer string) *int {
	t.Helper()

	calls := new(int)
	fio.ask = func(q string) any {
		*calls++

		if q != question {
			t.Errorf("asked %q, want %q", q, question)
		}

		return answer
	}

	return calls
}

func TestPerforce_QueryP4UserQueriesForUser(t *testing.T) {
	f := newPerforceFixture(t)
	f.perforce.SetUser(nil)
	askAnswering(t, f.io, "Enter P4 User:", "TEST_QUERY_USER")

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, strOf(f.perforce.GetUser()), "TEST_QUERY_USER")
}

func TestPerforce_QueryP4UserStoresResponseToQueryForUserWithWindows(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, true)
	f.perforce.SetUser(nil)
	calls := askAnswering(t, f.io, "Enter P4 User:", "TEST_QUERY_USER")
	f.process.Expects([]processmock.Expectation{processmock.Shell("p4 set"), processmock.Shell("p4 set P4USER=" + util.Escape("TEST_QUERY_USER"))}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, *calls, 1)
	assertComplete(t, f.process)
}

func TestPerforce_QueryP4UserStoresResponseToQueryForUserWithoutWindows(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, false)
	f.perforce.SetUser(nil)
	calls := askAnswering(t, f.io, "Enter P4 User:", "TEST_QUERY_USER")
	f.process.Expects([]processmock.Expectation{processmock.Shell("echo $P4USER"), processmock.Shell("export P4USER=" + util.Escape("TEST_QUERY_USER"))}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	expectEqual(t, *calls, 1)
	assertComplete(t, f.process)
}

func TestPerforce_QueryP4UserEscapesInjectionOnWindows(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, true)
	f.perforce.SetUser(nil)
	f.io.ask = func(string) any { return "foo && calc.exe" }
	f.process.Expects([]processmock.Expectation{processmock.Shell("p4 set"), processmock.Shell("p4 set P4USER=" + util.Escape("foo && calc.exe"))}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)
}

func TestPerforce_QueryP4UserEscapesInjectionOnUnix(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, false)
	f.perforce.SetUser(nil)
	f.io.ask = func(string) any { return "foo; id" }
	f.process.Expects([]processmock.Expectation{processmock.Shell("echo $P4USER"), processmock.Shell("export P4USER=" + util.Escape("foo; id"))}, true, nil)

	if err := f.perforce.QueryP4User(); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)
}

func queryPassword(t *testing.T, p *Perforce) string {
	t.Helper()

	password, err := p.QueryP4Password()
	if err != nil {
		t.Fatal(err)
	}

	return strOf(password)
}

func TestPerforce_QueryP4PasswordWithPasswordAlreadySet(t *testing.T) {
	f := newPerforceFixture(t)
	repoConfig := php.ArrayOf("depot", "depot", "branch", "branch", "p4user", "user", "p4password", "TEST_PASSWORD")

	p, err := newPerforce(repoConfig, "port", "path", f.process, false, newFakeIO(), "p4")
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, queryPassword(t, p), "TEST_PASSWORD")
}

func TestPerforce_QueryP4PasswordWithPasswordSetInP4VariablesWithWindowsOS(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, true)
	f.process.Expects([]processmock.Expectation{{Cmd: util.ShellCmd("p4 set"), Stdout: "P4PASSWD=TEST_P4VARIABLE_PASSWORD" + php.EOL}}, true, nil)

	expectEqual(t, queryPassword(t, f.perforce), "TEST_P4VARIABLE_PASSWORD")
}

func TestPerforce_QueryP4PasswordWithPasswordSetInP4VariablesNotWindowsOS(t *testing.T) {
	f := newPerforceFixture(t)
	f.createNewPerforceWithWindowsFlag(t, false)
	f.process.Expects([]processmock.Expectation{{Cmd: util.ShellCmd("echo $P4PASSWD"), Stdout: "TEST_P4VARIABLE_PASSWORD" + php.EOL}}, true, nil)

	expectEqual(t, queryPassword(t, f.perforce), "TEST_P4VARIABLE_PASSWORD")
}

func TestPerforce_QueryP4PasswordQueriesForPassword(t *testing.T) {
	f := newPerforceFixture(t)
	calls := askAnswering(t, f.io, "Enter password for Perforce user user: ", "TEST_QUERY_PASSWORD")

	expectEqual(t, queryPassword(t, f.perforce), "TEST_QUERY_PASSWORD")
	expectEqual(t, *calls, 1)
}

func assertClientSpec(t *testing.T, p *Perforce, withStream bool) {
	t.Helper()

	var b strings.Builder
	if err := p.WriteClientSpecToFile(&b); err != nil {
		t.Fatal(err)
	}

	expected := []string{
		"Client: composer_perforce_TEST_depot", php.EOL,
		"Update:", php.EOL,
		"Access:",
		"Owner:  user", php.EOL,
		"Description:",
		"  Created by user from composer.", php.EOL,
		"Root: path", php.EOL,
		"Options:  noallwrite noclobber nocompress unlocked modtime rmdir", php.EOL,
		"SubmitOptions:  revertunchanged", php.EOL,
		"LineEnd:  local", php.EOL,
	}
	if withStream {
		expected = append(expected, "Stream:", "  //depot/branch")
	} else {
		expected = append(expected, "View:  //depot/...  //composer_perforce_TEST_depot/...")
	}

	r := bufio.NewReader(strings.NewReader(b.String()))
	for _, want := range expected {
		line, _ := r.ReadString('\n')
		if !strings.HasPrefix(line, want) {
			t.Fatalf("line %q does not start with %q", line, want)
		}
	}

	if rest, _ := r.ReadString('\n'); rest != "" {
		t.Fatalf("unexpected line %q", rest)
	}
}

func TestPerforce_WriteP4ClientSpecWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	assertClientSpec(t, f.perforce, false)
}

func TestPerforce_WriteP4ClientSpecWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	// "Client: composer_perforce_TEST_depot" is a prefix of the stream
	// client's line, as in Composer's test
	assertClientSpec(t, f.perforce, true)
}

// Perforce::writeClientSpecToFile ends every line with PHP_EOL, "\r\n" on
// Windows, and getTags() splits p4's output at PHP_EOL.
func TestPerforce_WindowsEOL(t *testing.T) {
	php.SetEOLForTest(t, "\r\n")

	f := newPerforceFixture(t)
	f.setPerforceToStream()
	assertClientSpec(t, f.perforce, true)

	var b strings.Builder
	if err := f.perforce.WriteClientSpecToFile(&b); err != nil {
		t.Fatal(err)
	}
	spec := b.String()
	if strings.Count(spec, "\n") != strings.Count(spec, "\r\n") || !strings.HasPrefix(spec, "Client: composer_perforce_TEST_depot_branch\r\n\r\nUpdate: ") ||
		!strings.HasSuffix(spec, "LineEnd:  local\r\n\r\nStream:\r\n  //depot/branch\r\n") {
		t.Fatalf("spec %q", spec)
	}

	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "labels"), Stdout: strings.ReplaceAll(labelsOutput, "\n", "\r\n")},
	}, true, nil)
	assertTags(t, f.perforce, "//depot/branch")
}

func TestPerforce_IsLoggedIn(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{processmock.Cmd("p4", "-u", "user", "-p", "port", "login", "-s")}, true, nil)

	if _, err := f.perforce.IsLoggedIn(); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)
}

func branchesMaster(t *testing.T, p *Perforce) string {
	t.Helper()

	branches, err := p.GetBranches()
	if err != nil {
		t.Fatal(err)
	}

	master, _ := branches.GetString("master")

	return master
}

func TestPerforce_GetBranchesWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "streams", "//depot/..."), Stdout: "Stream //depot/branch mainline none 'branch'" + php.EOL},
		{Cmd: util.Cmd("p4", "-u", "user", "-p", "port", "changes", "//depot/branch/..."), Stdout: "Change 1234 on 2014/03/19 by Clark.Stuth@Clark.Stuth_test_client 'test changelist'"},
	}, true, nil)

	expectEqual(t, branchesMaster(t, f.perforce), "//depot/branch@1234")
}

func TestPerforce_GetBranchesWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-p", "port", "changes", "//depot/..."), Stdout: "Change 5678 on 2014/03/19 by Clark.Stuth@Clark.Stuth_test_client 'test changelist'"},
	}, true, nil)

	expectEqual(t, branchesMaster(t, f.perforce), "//depot@5678")
}

func assertTags(t *testing.T, p *Perforce, prefix string) {
	t.Helper()

	tags, err := p.GetTags()
	if err != nil {
		t.Fatal(err)
	}

	for _, label := range []string{"0.0.1", "0.0.2"} {
		got, _ := tags.GetString(label)
		expectEqual(t, got, prefix+"@"+label)
	}
}

const labelsOutput = "Label 0.0.1 2013/07/31 'First Label!'\nLabel 0.0.2 2013/08/01 'Second Label!'\n"

func TestPerforce_GetTagsWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot", "-p", "port", "labels"), Stdout: strings.ReplaceAll(labelsOutput, "\n", php.EOL)},
	}, true, nil)

	assertTags(t, f.perforce, "//depot")
}

func TestPerforce_GetTagsWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "labels"), Stdout: strings.ReplaceAll(labelsOutput, "\n", php.EOL)},
	}, true, nil)

	assertTags(t, f.perforce, "//depot/branch")
}

func TestPerforce_CheckStreamWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)

	result, err := f.perforce.CheckStream()
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, result, false)
	expectEqual(t, f.perforce.IsStream(), false)
}

func TestPerforce_CheckStreamWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-p", "port", "depots"), Stdout: "Depot depot 2013/06/25 stream /p4/1/depots/depot/... 'Created by Me'"},
	}, true, nil)

	result, err := f.perforce.CheckStream()
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, result, true)
	expectEqual(t, f.perforce.IsStream(), true)
}

// getComposerJSON is JsonFile::encode([...], JSON_FORCE_OBJECT).
const getComposerJSON = `{
    "name": "test/perforce",
    "description": "Basic project for testing",
    "minimum-stability": "dev",
    "autoload": {
        "psr-0": {}
    }
}`

func assertComposerInformation(t *testing.T, p *Perforce, identifier string) {
	t.Helper()

	result, err := p.GetComposerInformation(identifier)
	if err != nil {
		t.Fatal(err)
	}

	got, _ := php.JSONEncode(result, 0)
	expectEqual(t, got, `{"name":"test\/perforce","description":"Basic project for testing","minimum-stability":"dev","autoload":{"psr-0":[]}}`)
}

func TestPerforce_GetComposerInformationWithoutLabelWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot", "-p", "port", "print", "//depot/composer.json"), Stdout: getComposerJSON},
	}, true, nil)

	assertComposerInformation(t, f.perforce, "//depot")
}

func TestPerforce_GetComposerInformationWithLabelWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-p", "port", "files", "//depot/composer.json@0.0.1"), Stdout: "//depot/composer.json#1 - branch change 10001 (text)"},
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot", "-p", "port", "print", "//depot/composer.json@10001"), Stdout: getComposerJSON},
	}, true, nil)

	assertComposerInformation(t, f.perforce, "//depot@0.0.1")
}

func TestPerforce_GetComposerInformationWithoutLabelWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "print", "//depot/branch/composer.json"), Stdout: getComposerJSON},
	}, true, nil)

	assertComposerInformation(t, f.perforce, "//depot/branch")
}

func TestPerforce_GetComposerInformationWithLabelWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("p4", "-u", "user", "-p", "port", "files", "//depot/branch/composer.json@0.0.1"), Stdout: "//depot/composer.json#1 - branch change 10001 (text)"},
		{Cmd: util.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "print", "//depot/branch/composer.json@10001"), Stdout: getComposerJSON},
	}, true, nil)
	f.setPerforceToStream()

	assertComposerInformation(t, f.perforce, "//depot/branch@0.0.1")
}

func TestPerforce_SyncCodeBaseWithoutStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{processmock.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot", "-p", "port", "sync", "-f", "@label")}, true, nil)

	if err := f.perforce.SyncCodeBase(new("label")); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)
}

func TestPerforce_SyncCodeBaseWithStream(t *testing.T) {
	f := newPerforceFixture(t)
	f.setPerforceToStream()
	f.process.Expects([]processmock.Expectation{processmock.Cmd("p4", "-u", "user", "-c", "composer_perforce_TEST_depot_branch", "-p", "port", "sync", "-f", "@label")}, true, nil)

	if err := f.perforce.SyncCodeBase(new("label")); err != nil {
		t.Fatal(err)
	}

	assertComplete(t, f.process)
}

func TestPerforce_CheckServerExists(t *testing.T) {
	f := newPerforceFixture(t)
	f.process.Expects([]processmock.Expectation{processmock.Cmd("p4", "-p", "perforce.does.exist:port", "info", "-s")}, true, nil)

	result, err := CheckServerExists("perforce.does.exist:port", f.process)
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, result, true)
}

func TestPerforce_CheckServerExistsRejectsCommandExecutingPort(t *testing.T) {
	f := newPerforceFixture(t)
	// no process must be started at all for a rsh:/jsh: endpoint, as the p4 client would
	// execute it instead of connecting to a server
	f.process.Expects([]processmock.Expectation{}, true, nil)

	result, err := CheckServerExists("rsh:touch /tmp/pwned", f.process)
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, result, false)
	expectEqual(t, len(f.process.Log()), 0)
}

func TestPerforce_IsValidPortAcceptsNetworkEndpoints(t *testing.T) {
	for _, port := range []string{"1666", "perforce", "p4.example.org:1666", "perforce.does.exist:port", "tcp:p4.example.org:1666", "tcp4:p4.example.org:1666", "ssl:p4.example.org:1666", "SSL:p4.example.org:1666", "ssl64:[2001:db8::1]:1666", "tcp6:[::1]:1666"} {
		if !IsValidPort(port) {
			t.Errorf("%q rejected", port)
		}
	}
}

func TestPerforce_IsValidPortRejectsNonNetworkEndpoints(t *testing.T) {
	for _, port := range []string{
		// rsh:/jsh: make the p4 client run the rest of the value as a local command
		"rsh:/tmp/evil.sh", "rsh:evil", "RSH:evil", " rsh:evil", "jsh:evil", "JsH:evil", "rsh :evil",
		// not valid endpoints either way
		"tcp:p4.example.org:1666; touch /tmp/pwned", "https://example.org/vendor/pkg.git", "-p1666", "",
	} {
		if IsValidPort(port) {
			t.Errorf("%q accepted", port)
		}
	}
}

func TestPerforce_CreatingPerforceWithCommandExecutingPortThrows(t *testing.T) {
	f := newPerforceFixture(t)

	_, err := NewPerforce(f.repoConfig, "rsh:touch /tmp/pwned", testPath, f.process, false, f.io)

	var se *util.SecurityError
	if !errors.As(err, &se) || !strings.Contains(se.Message, "Invalid Perforce port (rsh:touch /tmp/pwned)") {
		t.Fatalf("got %v", err)
	}
}

func TestPerforce_CheckServerClientError(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{{Cmd: util.Cmd("p4", "-p", "perforce.does.exist:port", "info", "-s"), Return: 127}}, true, nil)

	result, err := CheckServerExists("perforce.does.exist:port", process)
	if err != nil {
		t.Fatal(err)
	}

	expectEqual(t, result, false)
	assertComplete(t, process)
}

type fakePerforceFS struct{ removed []string }

func (f *fakePerforceFS) Remove(file string) (bool, error) {
	f.removed = append(f.removed, file)

	return true, nil
}

func TestPerforce_CleanupClientSpecShouldDeleteClient(t *testing.T) {
	f := newPerforceFixture(t)
	fs := &fakePerforceFS{}
	f.perforce.SetFilesystem(fs)

	testClient := f.perforce.GetClient()
	f.process.Expects([]processmock.Expectation{processmock.Cmd("p4", "-u", testP4User, "-p", testPort, "client", "-d", testClient)}, true, nil)

	if err := f.perforce.CleanupClientSpec(); err != nil {
		t.Fatal(err)
	}

	if len(fs.removed) != 1 || fs.removed[0] != f.perforce.GetP4ClientSpec() {
		t.Fatalf("removed %v", fs.removed)
	}

	assertComplete(t, f.process)
}
