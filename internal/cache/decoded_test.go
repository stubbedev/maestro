package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/php"
)

// Decoded keeps at most maxSlots slots, dropping the least recently
// written.
func TestDecoded_BoundsItsSlots(t *testing.T) {
	dir := t.TempDir()
	d := NewDecoded("test\n", 0, 2)
	d.Use(dir)
	decode := func(json string) (any, error) { return php.JSONDecode(json, true) }
	json := `{"a": "` + strings.Repeat("x", 64) + `"}`

	for i, source := range []string{"one", "two", "three"} {
		_, store, err := d.Decode(source, json, decode)
		if err != nil || store == nil {
			t.Fatalf("%s: store %v, %v", source, store != nil, err)
		}
		store()
		// written i hours after the first
		sum := sha256.Sum256([]byte(source))
		at := time.Now().Add(time.Duration(i-3) * time.Hour)
		if err := os.Chtimes(filepath.Join(dir, hex.EncodeToString(sum[:16])+".bin"), at, at); err != nil && i < 2 {
			t.Fatal(err)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 2 {
		t.Fatalf("%d slots, want 2", len(entries))
	}
	// "one" went: it is decoded and stored again
	if _, store, _ := d.Decode("one", json, decode); store == nil {
		t.Error("the oldest slot was kept")
	}
	if _, store, _ := d.Decode("three", json, decode); store != nil {
		t.Error("the newest slot was not kept")
	}
}
