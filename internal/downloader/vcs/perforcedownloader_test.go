// Ports tests/Composer/Test/Downloader/PerforceDownloaderTest.php.

package vcs

import (
	"slices"
	"strings"
	"testing"

	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

// fakeVcsRepository is the VcsRepository mock of the test: only
// getRepoConfig is stubbed, and its calls are counted.
type fakeVcsRepository struct {
	config *php.Array
	calls  int
}

func (r *fakeVcsRepository) RepoName() string { return "vcs repo" }
func (r *fakeVcsRepository) Class() string    { return `Composer\Repository\VcsRepository` }

func (r *fakeVcsRepository) RepoConfig() *php.Array {
	r.calls++

	return r.config
}

// fakePerforce is the Composer\Util\Perforce mock, recording calls.
type fakePerforce struct {
	calls []string
	label *string
}

func (f *fakePerforce) record(call string) { f.calls = append(f.calls, call) }

func (f *fakePerforce) InitializePath(path string) error {
	f.record("initializePath " + path)

	return nil
}

func (f *fakePerforce) SetStream(stream string) { f.record("setStream " + stream) }

func (f *fakePerforce) P4Login() error {
	f.record("p4Login")

	return nil
}

func (f *fakePerforce) WriteP4ClientSpec() error {
	f.record("writeP4ClientSpec")

	return nil
}

func (f *fakePerforce) ConnectClient() error {
	f.record("connectClient")

	return nil
}

func (f *fakePerforce) SyncCodeBase(sourceReference *string) error {
	f.record("syncCodeBase")
	f.label = sourceReference

	return nil
}

func (f *fakePerforce) CleanupClientSpec() error {
	f.record("cleanupClientSpec")

	return nil
}

func (f *fakePerforce) GetCommitLogs(string, string) (string, bool, error) { return "", false, nil }

// recordingIO counts writeError calls.
type recordingIO struct {
	*mio.NullIO
	errors []string
}

func (r *recordingIO) WriteError(message string, _ bool, _ mio.Verbosity) {
	r.errors = append(r.errors, message)
}

type perforceTest struct {
	io         *recordingIO
	repository *fakeVcsRepository
	pkg        *pkg.CompletePackage
	downloader *PerforceDownloader
	testPath   string
}

func newPerforceTest(t *testing.T) *perforceTest {
	t.Helper()

	pt := &perforceTest{
		io:         &recordingIO{NullIO: mio.NewNullIO()},
		repository: &fakeVcsRepository{config: php.ArrayOf("url", "TEST_URL", "p4user", "TEST_USER")},
		pkg:        sourcePackage("1.0.0.0", "1.0.0", ""),
		testPath:   t.TempDir(),
	}
	noError(t, pt.pkg.SetRepository(pt.repository))
	pt.downloader = NewPerforceDownloader(Deps{IO: pt.io, Config: newConfig(t, "home", pt.testPath), Process: processmock.New(), Filesystem: &fakeFS{}})

	return pt
}

func TestPerforceDownloader_InitPerforceInstantiatesANewPerforceObject(t *testing.T) {
	pt := newPerforceTest(t)

	noError(t, pt.downloader.InitPerforce(pt.pkg, pt.testPath, "SOURCE_REF"))

	if pt.downloader.perforce == nil {
		t.Fatal("no Perforce instance")
	}
}

func TestPerforceDownloader_InitPerforceDoesNothingIfPerforceAlreadySet(t *testing.T) {
	pt := newPerforceTest(t)
	pt.downloader.SetPerforce(&fakePerforce{})

	noError(t, pt.downloader.InitPerforce(pt.pkg, pt.testPath, "SOURCE_REF"))

	if pt.repository.calls != 0 {
		t.Fatalf("getRepoConfig called %d times", pt.repository.calls)
	}
}

func testPerforceDoInstall(t *testing.T, ref string, label *string) {
	t.Helper()

	pt := newPerforceTest(t)
	pt.pkg.SetSourceReference(pkg.Str(ref))

	perforce := &fakePerforce{}
	pt.downloader.SetPerforce(perforce)

	noError(t, pt.downloader.DoInstall(pt.pkg, pt.testPath, "url"))

	if len(pt.io.errors) != 1 || !strings.Contains(pt.io.errors[0], "Cloning "+ref) {
		t.Fatalf("writeError calls: %q", pt.io.errors)
	}

	want := []string{"initializePath " + pt.testPath, "setStream " + ref, "p4Login", "writeP4ClientSpec", "connectClient", "syncCodeBase", "cleanupClientSpec"}
	if !slices.Equal(perforce.calls, want) {
		t.Fatalf("calls %q, want %q", perforce.calls, want)
	}

	if (label == nil) != (perforce.label == nil) || (label != nil && *label != *perforce.label) {
		t.Fatalf("syncCodeBase label %v, want %v", perforce.label, label)
	}
}

func TestPerforceDownloader_DoInstallWithTag(t *testing.T) {
	label := "123"
	testPerforceDoInstall(t, "SOURCE_REF@123", &label)
}

func TestPerforceDownloader_DoInstallWithNoTag(t *testing.T) {
	testPerforceDoInstall(t, "SOURCE_REF", nil)
}
