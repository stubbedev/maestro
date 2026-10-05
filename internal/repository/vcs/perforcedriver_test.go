package vcs

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// Ports tests/Composer/Test/Repository/Vcs/PerforceDriverTest.php.

const (
	perforceTestURL    = "TEST_PERFORCE_URL"
	perforceTestDepot  = "TEST_DEPOT_CONFIG"
	perforceTestBranch = "TEST_BRANCH_CONFIG"
)

// fakePerforce is the Perforce mock: it counts calls and answers
// getComposerInformation.
type fakePerforce struct {
	calls        map[string]int
	composerInfo *php.Array
	infoPath     string
}

func (f *fakePerforce) called(name string) error {
	f.calls[name]++

	return nil
}

func (f *fakePerforce) P4Login() error             { return f.called("p4Login") }
func (f *fakePerforce) CheckStream() (bool, error) { return false, f.called("checkStream") }
func (f *fakePerforce) WriteP4ClientSpec() error   { return f.called("writeP4ClientSpec") }
func (f *fakePerforce) ConnectClient() error       { return f.called("connectClient") }
func (f *fakePerforce) CleanupClientSpec() error   { return f.called("cleanupClientSpec") }
func (f *fakePerforce) GetUser() *string           { return nil }

func (f *fakePerforce) GetComposerInformation(identifier string) (*php.Array, error) {
	f.infoPath = identifier

	return f.composerInfo, f.called("getComposerInformation")
}

func (f *fakePerforce) GetFileContent(string, string) (string, bool, error) { return "", false, nil }

func (f *fakePerforce) GetBranches() (*php.Array, error) { return php.NewArray(), nil }

func (f *fakePerforce) GetTags() (*php.Array, error) { return php.NewArray(), nil }

func perforceRepoConfig() *php.Array {
	return php.ArrayOf("url", perforceTestURL, "depot", perforceTestDepot, "branch", perforceTestBranch)
}

func newPerforceDriverTest(t *testing.T) (*PerforceDriver, *fakePerforce) {
	t.Helper()

	perforce := &fakePerforce{calls: map[string]int{}}
	driver := NewPerforceDriver(perforceRepoConfig(), deps(newTestIO(), newConfig(t, t.TempDir()), httpmock.New(), processmock.New()))
	driver.perforce = perforce

	return driver, perforce
}

func TestPerforceDriver_InitializeCapturesVariablesFromRepoConfig(t *testing.T) {
	t.Parallel()

	driver := NewPerforceDriver(perforceRepoConfig(), deps(newTestIO(), newConfig(t, t.TempDir()), httpmock.New(), processmock.New()))
	noErr(t, driver.Initialize())

	if driver.URL() != perforceTestURL || driver.Depot() != perforceTestDepot || driver.Branch() != perforceTestBranch {
		t.Fatalf("got %q %q %q", driver.URL(), driver.Depot(), driver.Branch())
	}
}

func TestPerforceDriver_InitializeLogsInAndConnectsClient(t *testing.T) {
	t.Parallel()

	driver, perforce := newPerforceDriverTest(t)
	noErr(t, driver.Initialize())

	for _, m := range []string{"p4Login", "checkStream", "writeP4ClientSpec", "connectClient"} {
		if perforce.calls[m] != 1 {
			t.Errorf("%s called %d times", m, perforce.calls[m])
		}
	}
}

func TestPerforceDriver_HasComposerFileReturnsFalseOnNoComposerFile(t *testing.T) {
	t.Parallel()

	driver, perforce := newPerforceDriverTest(t)
	perforce.composerInfo = php.NewArray()
	noErr(t, driver.Initialize())

	ok, err := driver.HasComposerFile("TEST_IDENTIFIER")
	noErr(t, err)

	if ok || perforce.infoPath != "//"+perforceTestDepot+"/TEST_IDENTIFIER" {
		t.Fatalf("got %v for %q", ok, perforce.infoPath)
	}
}

func TestPerforceDriver_HasComposerFileReturnsTrueWithOneOrMoreComposerFiles(t *testing.T) {
	t.Parallel()

	driver, perforce := newPerforceDriverTest(t)
	perforce.composerInfo = php.ListOf("")
	noErr(t, driver.Initialize())

	ok, err := driver.HasComposerFile("TEST_IDENTIFIER")
	noErr(t, err)

	if !ok || perforce.infoPath != "//"+perforceTestDepot+"/TEST_IDENTIFIER" {
		t.Fatalf("got %v for %q", ok, perforce.infoPath)
	}
}

// TestPerforceDriver_SupportsReturnsFalseNoDeepCheck: supports() simply
// returns false (and outputs nothing).
func TestPerforceDriver_SupportsReturnsFalseNoDeepCheck(t *testing.T) {
	t.Parallel()

	process := processmock.New()
	process.Expects(nil, true, nil)
	ioi := newTestIO()

	ok, err := perforceDriverType.Supports(deps(ioi, newConfig(t, t.TempDir()), nil, process), "existing.url", false)
	if err != nil || ok {
		t.Fatalf("got %v, %v", ok, err)
	}

	if len(ioi.messages()) != 0 || len(process.Log()) != 0 {
		t.Fatalf("unexpected output %q, commands %q", ioi.messages(), process.Log())
	}
}

func TestPerforceDriver_Cleanup(t *testing.T) {
	t.Parallel()

	driver, perforce := newPerforceDriverTest(t)
	noErr(t, driver.Cleanup())

	if perforce.calls["cleanupClientSpec"] != 1 || driver.perforce != nil {
		t.Fatalf("cleanupClientSpec called %d times", perforce.calls["cleanupClientSpec"])
	}
}
