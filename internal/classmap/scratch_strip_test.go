package classmap

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestScratchStrip(t *testing.T) {
	list := os.Getenv("STRIP_LIST")
	if list == "" {
		t.Skip()
	}
	data, _ := os.ReadFile(list)
	files := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	sums, _ := os.ReadFile(os.Getenv("STRIP_OUT"))
	want := strings.Split(string(sums), "\n")
	bad := 0
	for i, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		h := md5.Sum(StripWhitespace(src, true))
		if hex.EncodeToString(h[:]) != want[i] {
			bad++
			t.Errorf("%s", f)
			if bad > 30 {
				t.FailNow()
			}
		}
	}
	t.Logf("%d files", len(files))
}
