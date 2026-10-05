// Ports tests/Composer/Test/AllFunctionalTest.php's testIntegration for
// the fixtures in testdata/functional (copied verbatim from
// tests/Composer/Test/Fixtures/functional). Composer runs them against a
// built composer.phar in a subprocess; here they run in process through
// the Application, against the network and git like Composer's, so they
// only run with MAESTRO_E2E=1.

package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/command/commandtest"
	"github.com/stubbedev/maestro/internal/composer"
	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

var functionalSection = php.MustCompile(`#(?:^|\n*)--([A-Z-]+)--\n#`)

// parseFunctionalTestFile ports parseTestFile.
func parseFunctionalTestFile(t *testing.T, file string) map[string]string {
	t.Helper()
	contents, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := functionalSection.Split(string(contents), -1, php.PregSplitDelimCapture)
	if err != nil {
		t.Fatal(err)
	}
	data := map[string]string{}
	section, haveSection := "", false
	for _, token := range tokens {
		if token == "" && !haveSection {
			continue
		}

		// Handle section headers.
		if !haveSection {
			section, haveSection = token, true

			continue
		}

		sectionData := token

		// Allow sections to validate, or modify their section data.
		switch section {
		case "EXPECT-EXIT-CODE":
			sectionData = strconv.FormatInt(php.ToInt(sectionData), 10)
		case "RUN", "EXPECT", "EXPECT-REGEX", "EXPECT-REGEXES":
			sectionData = strings.TrimSpace(sectionData)
		case "TEST":
		default:
			t.Fatalf(`Unknown section "%s". Allowed sections: "RUN", "EXPECT", "EXPECT-EXIT-CODE", "EXPECT-REGEX", "EXPECT-REGEXES". Section headers must be written as "--HEADER_NAME--".`, section)
		}

		data[section] = sectionData
		haveSection = false
	}

	// validate data
	if _, ok := data["RUN"]; !ok {
		t.Fatal(`The test file must have a section named "RUN".`)
	}
	_, e1 := data["EXPECT"]
	_, e2 := data["EXPECT-REGEX"]
	_, e3 := data["EXPECT-REGEXES"]
	if !e1 && !e2 && !e3 {
		t.Fatal(`The test file must have a section named "EXPECT", "EXPECT-REGEX", or "EXPECT-REGEXES".`)
	}

	return data
}

// cleanFunctionalOutput ports cleanOutput: backspaces erase, \r is dropped.
func cleanFunctionalOutput(output string) string {
	processed := make([]byte, 0, len(output))
	for i := range len(output) {
		switch output[i] {
		case '\x08':
			if len(processed) > 0 {
				processed = processed[:len(processed)-1]
			}
		case '\r':
		default:
			processed = append(processed, output[i])
		}
	}

	return string(processed)
}

var percentPattern = php.MustCompile(`{%(.+?)%}`)

// matchFunctionalExpect is the EXPECT comparison: byte for byte, with
// %regex% placeholders.
func matchFunctionalExpect(t *testing.T, expected, output string) {
	t.Helper()
	line := 1
	i, j := 0, 0
	for i < len(expected) {
		if expected[i] == '\n' {
			line++
		}
		if expected[i] == '%' {
			m, err := percentPattern.MatchStrictGroups(expected[i:])
			if err != nil || m == nil {
				t.Fatalf("Failed to match %%...%% in %s", expected[i:])
			}
			regex := m.Get(1)
			om, err := php.MustCompile("{" + regex + "}").Match(output[j:])
			if err == nil && om != nil {
				i += len(regex) + 2
				j += len(om.Get(0))

				continue
			}
			t.Fatalf("Failed to match pattern %s at line %d / abs offset %d: %s\n\nOutput:\n%s", regex, line, i, functionalFirstLine(output[j:]), output)
		}
		if j >= len(output) || expected[i] != output[j] {
			t.Fatalf("Output does not match expectation at line %d / abs offset %d: \n-%s\n+%s\n\nOutput:\n%s", line, i, functionalFirstLine(expected[i:]), functionalFirstLine(output[min(j, len(output)):]), output)
		}
		i++
		j++
	}
}

func functionalFirstLine(s string) string {
	if k := strings.IndexByte(s, '\n'); k >= 0 {
		s = s[:k]
	}

	return s[:min(len(s), 100)]
}

func TestAllFunctional_Integration(t *testing.T) {
	if os.Getenv("MAESTRO_E2E") != "1" {
		t.Skip("functional fixtures need the network and git; set MAESTRO_E2E=1")
	}
	files, err := filepath.Glob("testdata/functional/*.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, testFile := range files {
		abs, err := filepath.Abs(testFile)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(filepath.Base(testFile), func(t *testing.T) {
			runFunctionalFixture(t, abs)
		})
	}
}

func runFunctionalFixture(t *testing.T, testFile string) {
	testData := parseFunctionalTestFile(t, testFile)
	testDir := commandtest.UniqueTmpDirectory(t)
	t.Cleanup(func() {
		_ = os.RemoveAll(testDir + "home")
		_ = os.RemoveAll(testDir + "cache")
	})

	// if a dir is present with the name of the .test file (without .test), we
	// copy all its contents in the $testDir to be used to run the test with
	if setupDir := strings.TrimSuffix(testFile, ".test"); functionalIsDir(setupDir) {
		if err := os.CopyFS(testDir, os.DirFS(setupDir)); err != nil {
			t.Fatal(err)
		}
	}

	prevCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prevCwd) })
	if err := os.Chdir(testDir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("COMPOSER_HOME", testDir+"home")
	t.Setenv("COMPOSER_CACHE_DIR", testDir+"cache")
	// The subprocess Composer starts inherits these from the test process
	// and keeps its own process state; restore what the run changes.
	for _, name := range []string{"COMPOSER", "COMPOSER_ROOT_VERSION"} {
		if v, ok := os.LookupEnv(name); ok {
			t.Cleanup(func() { util.PutEnv(name, v) })
		} else {
			t.Cleanup(func() { util.ClearEnv(name) })
		}
	}

	in, err := console.NewStringInput("--no-ansi " + testData["RUN"])
	if err != nil {
		t.Fatal(err)
	}
	// the subprocess has no terminal
	in.SetInteractive(false)

	var buf bytes.Buffer
	out := console.NewStreamOutput(&buf, console.VerbosityNormal, new(false), console.NewOutputFormatter(false, composer.CreateAdditionalStyles()...))

	app := commandtest.NewApplication()
	app.SetAutoExit(false)
	exitCode, err := app.Run(in, out)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, buf.String())
	}
	output := buf.String()

	if expected, ok := testData["EXPECT"]; ok {
		matchFunctionalExpect(t, expected, strings.TrimSpace(cleanFunctionalOutput(output)))
	}
	if regex, ok := testData["EXPECT-REGEX"]; ok {
		if m, err := php.MustCompile(regex).IsMatch(cleanFunctionalOutput(output)); err != nil || !m {
			t.Fatalf("output does not match %s:\n%s", regex, output)
		}
	}
	if regexes, ok := testData["EXPECT-REGEXES"]; ok {
		cleanOutput := cleanFunctionalOutput(output)
		for regex := range strings.SplitSeq(regexes, "\n") {
			if m, err := php.MustCompile(regex).IsMatch(cleanOutput); err != nil || !m {
				t.Fatalf("output does not match %s:\nOutput: %s", regex, output)
			}
		}
	}
	if code, ok := testData["EXPECT-EXIT-CODE"]; ok {
		if strconv.Itoa(exitCode) != code {
			t.Fatalf("exit code %d, want %s", exitCode, code)
		}
	}
}

func functionalIsDir(path string) bool {
	st, err := os.Stat(path)

	return err == nil && st.IsDir()
}
