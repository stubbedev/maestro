// Ports tests/Composer/Test/AllFunctionalTest.php: the functional fixtures
// (internal/composer/testdata/Fixtures/functional, copied verbatim from
// Composer) run against the built maestro binary. They need php, plugins
// and the network, so they only run with MAESTRO_E2E=1.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

const functionalDir = "../../internal/composer/testdata/Fixtures/functional"

func TestAllFunctional(t *testing.T) {
	if os.Getenv("MAESTRO_E2E") == "" {
		t.Skip("set MAESTRO_E2E=1 to run the functional fixtures (php, plugins and the network)")
	}

	bin := filepath.Join(t.TempDir(), "maestro"+exeSuffix)
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	files, err := filepath.Glob(filepath.Join(functionalDir, "*.test"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) { runFunctional(t, bin, file) })
	}
}

func runFunctional(t *testing.T, bin, testFile string) {
	data, err := os.ReadFile(testFile)
	if err != nil {
		t.Fatal(err)
	}
	testData, err := parseFunctionalTest(string(data))
	if err != nil {
		t.Fatal(err)
	}
	testDir := t.TempDir()

	// if a dir is present with the name of the .test file (without .test), we
	// copy all its contents in the $testDir to be used to run the test with
	if setup := strings.TrimSuffix(testFile, ".test"); isDir(setup) {
		if err := os.CopyFS(testDir, os.DirFS(setup)); err != nil {
			t.Fatal(err)
		}
	}

	quoted, err := php.Escapeshellarg(bin)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		// escapeshellarg() on Windows: double quotes (the temporary
		// directory's path has no '"', '%' or '!' it would blank out).
		quoted = `"` + bin + `"`
	}
	cmd := shellCommand(quoted + " --no-ansi " + testData["RUN"])
	cmd.Dir = testDir
	cmd.Env = append(os.Environ(), "COMPOSER_HOME="+testDir+"home", "COMPOSER_CACHE_DIR="+testDir+"cache")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	exitCode := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError) //nolint:errorlint // exec returns it unwrapped
		if !ok {
			t.Fatal(err)
		}
		exitCode = ee.ExitCode()
	}
	output := out.String()

	if expected, ok := testData["EXPECT"]; ok {
		if err := matchExpectation(expected, php.Trim(cleanOutput(output))); err != nil {
			t.Fatalf("%v\nOutput:\n%s", err, output)
		}
	}
	if re, ok := testData["EXPECT-REGEX"]; ok {
		if ok, err := php.MustCompile(re).IsMatch(cleanOutput(output)); err != nil || !ok {
			t.Fatalf("output does not match %s\nOutput:\n%s", re, output)
		}
	}
	if res, ok := testData["EXPECT-REGEXES"]; ok {
		for re := range strings.SplitSeq(res, "\n") {
			if ok, err := php.MustCompile(re).IsMatch(cleanOutput(output)); err != nil || !ok {
				t.Fatalf("output does not match %s\nOutput:\n%s", re, output)
			}
		}
	}
	if code, ok := testData["EXPECT-EXIT-CODE"]; ok {
		if want := int(php.ToInt(code)); want != exitCode {
			t.Fatalf("exit code %d, want %d\nOutput:\n%s", exitCode, want, output)
		}
	}
}

func isDir(path string) bool {
	st, err := os.Stat(path)

	return err == nil && st.IsDir()
}

var expectPattern = php.MustCompile(`{%(.+?)%}`)

// matchExpectation compares output with expected, where %regex% matches a
// run of the output.
func matchExpectation(expected, output string) error {
	line := 1
	i, j := 0, 0
	for i < len(expected) {
		if expected[i] == '\n' {
			line++
		}
		if expected[i] == '%' {
			m, err := expectPattern.Match(expected[i:])
			if err != nil || m == nil {
				return &mismatch{"Failed to match %...% in " + expected[i:]}
			}
			regex := m.Get(1)
			re, err := php.Compile("{" + regex + "}")
			if err != nil {
				return err
			}
			if om, err := re.Match(output[j:]); err == nil && om != nil {
				i += len(regex) + 2
				j += len(om.Get(0))

				continue
			}

			return &mismatch{"Failed to match pattern " + regex + " at line " + strconv.Itoa(line) + " / abs offset " + strconv.Itoa(i)}
		}
		if j >= len(output) || expected[i] != output[j] {
			return &mismatch{"Output does not match expectation at line " + strconv.Itoa(line) + " / abs offset " + strconv.Itoa(i)}
		}
		i++
		j++
	}

	return nil
}

type mismatch struct{ msg string }

func (m *mismatch) Error() string { return m.msg }

var sectionSplit = php.MustCompile(`#(?:^|\n*)--([A-Z-]+)--\n#`)

// parseFunctionalTest ports parseTestFile.
func parseFunctionalTest(content string) (map[string]string, error) {
	tokens, err := sectionSplit.Split(content, -1, php.PregSplitDelimCapture)
	if err != nil {
		return nil, err
	}
	data := map[string]string{}
	section := ""
	inSection := false
	for _, token := range tokens {
		if token == "" && !inSection {
			continue
		}
		if !inSection {
			section, inSection = token, true

			continue
		}
		switch section {
		case "RUN", "EXPECT", "EXPECT-REGEX", "EXPECT-REGEXES":
			token = php.Trim(token)
		case "EXPECT-EXIT-CODE", "TEST":
		default:
			return nil, &mismatch{`Unknown section "` + section + `".`}
		}
		data[section] = token
		inSection = false
	}
	if _, ok := data["RUN"]; !ok {
		return nil, &mismatch{`The test file must have a section named "RUN".`}
	}

	return data, nil
}

// cleanOutput applies backspaces and drops carriage returns.
func cleanOutput(output string) string {
	var b []byte
	for i := range len(output) {
		switch output[i] {
		case '\x08':
			if len(b) > 0 {
				b = b[:len(b)-1]
			}
		case '\r':
		default:
			b = append(b, output[i])
		}
	}

	return string(b)
}
