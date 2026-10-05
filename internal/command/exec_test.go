// Ports tests/Composer/Test/Command/ExecCommandTest.php.

package command_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
)

func TestExecCommand_ListThrowsIfNoBinariesExist(t *testing.T) {
	composerDir := commandtest.InitTempComposer(t, nil, nil, nil, true)

	composerBinDir := composerDir + "/vendor/bin"

	tester := commandtest.GetApplicationTester(t)
	_, err := tester.RunArgs(commandtest.Options{}, "command", "exec", "--list", true)
	want := "No binaries found in composer.json or in bin-dir (" + composerBinDir + ")"
	if err == nil || err.Error() != want {
		t.Fatalf("err %v, want %q", err, want)
	}
}

func TestExecCommand_List(t *testing.T) {
	composerDir := commandtest.InitTempComposer(t, `{"bin": ["a"]}`, nil, nil, true)

	composerBinDir := composerDir + "/vendor/bin"
	if err := os.MkdirAll(composerBinDir, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"b", "b.bat", "c"} {
		if err := os.WriteFile(composerBinDir+"/"+f, nil, 0o666); err != nil {
			t.Fatal(err)
		}
	}

	tester := commandtest.GetApplicationTester(t)
	if _, err := tester.RunArgs(commandtest.Options{}, "command", "exec", "--list", true); err != nil {
		t.Fatal(err)
	}

	output := tester.Display(true)

	want := "Available binaries:\n- b\n- c\n- a (local)"
	if got := strings.TrimSpace(output); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
