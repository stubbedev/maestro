package classmap

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The goldens are written by tools/oracle/classmap/*.php, which run the real
// composer/class-map-generator 1.7.3 on PHP 8.4.

// record is record() of tools/oracle/classmap/common.php for the Go port.
func record(t *testing.T, path, base string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := md5.Sum(StripWhitespace(src, true))
	line := hex.EncodeToString(sum[:]) + " "
	classes, err := FindClasses(path)
	if err != nil {
		var e *Exception
		if !errors.As(err, &e) || e.Class != classRuntime {
			t.Fatalf("%s: unexpected error %v", path, err)
		}
		msg, _, _ := strings.Cut(e.Message, "\nThe following message may be helpful:")

		return line + "!" + hex.EncodeToString([]byte(strings.ReplaceAll(msg, base, "")))
	}

	return line + hex.EncodeToString([]byte(strings.Join(classes, "\x00")))
}

// describeRecord decodes a record for failure messages.
func describeRecord(r string) string {
	md5sum, rest, _ := strings.Cut(r, " ")
	if msg, ok := strings.CutPrefix(rest, "!"); ok {
		b, _ := hex.DecodeString(msg)

		return md5sum + " error " + strconv.Quote(string(b))
	}
	b, _ := hex.DecodeString(rest)

	return md5sum + " classes " + strconv.Quote(string(b))
}

func TestOracleFiles(t *testing.T) {
	var golden []struct {
		Path   string  `json:"path"`
		Record string  `json:"record"`
		Strip  *string `json:"strip"`
	}
	data, err := os.ReadFile("testdata/oracle/files.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if len(golden) < 90 {
		t.Fatalf("only %d golden files", len(golden))
	}
	for _, g := range golden {
		path := "testdata/" + g.Path
		if g.Strip != nil {
			want, _ := base64.StdEncoding.DecodeString(*g.Strip)
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := StripWhitespace(src, true); !bytes.Equal(got, want) {
				t.Errorf("%s: php_strip_whitespace\n got %q\nwant %q", g.Path, got, want)
			}
		}
		if got := record(t, path, "testdata/"); got != g.Record {
			t.Errorf("%s:\n got %s\nwant %s", g.Path, describeRecord(got), describeRecord(g.Record))
		}
	}
}

// TestOracleCorpus checks every .php file of the Composer sources, its
// dependencies and its tests. It needs .ref (see ref-sync).
func TestOracleCorpus(t *testing.T) {
	root := "../../.ref/composer"
	if _, err := os.Stat(root); err != nil {
		t.Skip(".ref/composer is not available")
	}
	f, err := os.Open("testdata/oracle/corpus.golden")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		path, want, _ := strings.Cut(sc.Text(), "\t")
		if got := record(t, root+"/"+path, root+"/"); got != want {
			t.Errorf("%s:\n got %s\nwant %s", path, describeRecord(got), describeRecord(want))
		}
		n++
	}
	if n < 1000 {
		t.Fatalf("only %d corpus files", n)
	}
}

func TestOracleRandom(t *testing.T) {
	checkRandom(t, "testdata/oracle")
}

// TestOracleRandomLive generates a fresh random set with PHP
// (MAESTRO_ORACLE_LIVE=1, MAESTRO_ORACLE_SEED and MAESTRO_ORACLE_COUNT
// optional) and compares.
func TestOracleRandomLive(t *testing.T) {
	if os.Getenv("MAESTRO_ORACLE_LIVE") == "" {
		t.Skip("set MAESTRO_ORACLE_LIVE=1 to run against php")
	}
	dir := t.TempDir()
	seed := cmp(os.Getenv("MAESTRO_ORACLE_SEED"), "42")
	count := cmp(os.Getenv("MAESTRO_ORACLE_COUNT"), "50000")
	cmd := exec.Command("php", "../../tools/oracle/classmap/random.php", seed, count, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	checkRandom(t, dir)
}

func cmp(v, def string) string {
	if v == "" {
		return def
	}

	return v
}

func checkRandom(t *testing.T, dir string) {
	t.Helper()
	cases, err := os.ReadFile(filepath.Join(dir, "random.bin"))
	if err != nil {
		t.Fatal(err)
	}
	golden, err := os.ReadFile(filepath.Join(dir, "random.golden"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(golden), "\n"), "\n")
	tmp := filepath.Join(t.TempDir(), "case.php")
	failures := 0
	for i := 0; len(cases) > 0; i++ {
		n := binary.BigEndian.Uint32(cases)
		src := cases[4 : 4+n]
		cases = cases[4+n:]
		if err := os.WriteFile(tmp, src, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := record(t, tmp, tmp); got != lines[i] {
			t.Errorf("case %d %q:\n got %s\nwant %s\nstrip %q", i, src, describeRecord(got), describeRecord(lines[i]), StripWhitespace(src, true))
			if failures++; failures > 20 {
				t.FailNow()
			}
		}
		if i == len(lines)-1 && len(cases) > 0 {
			t.Fatal("more cases than golden lines")
		}
	}
}
