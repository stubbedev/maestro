package fsstate

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o666); err != nil {
		t.Fatal(err)
	}
}

func mustStat(t *testing.T, path string) ID {
	t.Helper()
	id, ok := Stat(path)
	if !ok {
		t.Fatalf("no ID for %s", path)
	}

	return id
}

// A change of mode alone, or a rewrite of the same size that restores the
// modification time (touch -r, rsync --times), changes the ID: its change
// time does.
func TestID_ChangesWithoutContentOrTimeChanges(t *testing.T) {
	if !Known() {
		t.Skip("no IDs here")
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "f")
	writeFile(t, path, "one")
	before := mustStat(t, path)
	if !before.IsRegular() {
		t.Error("a regular file is not IsRegular")
	}
	if dirID := mustStat(t, filepath.Dir(path)); dirID.IsRegular() {
		t.Error("a directory is IsRegular")
	}

	time.Sleep(10 * time.Millisecond)
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if mustStat(t, path) == before {
		t.Error("the ID did not change with the mode")
	}

	modified := time.Unix(0, before.Mtime)
	time.Sleep(10 * time.Millisecond)
	writeFile(t, path, "two")
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatal(err)
	}
	after := mustStat(t, path)
	if after.Mtime != before.Mtime || after.Size != before.Size {
		t.Fatalf("the rewrite did not keep the size and modification time")
	}
	if after == before {
		t.Error("the ID did not change with a same-size rewrite keeping the modification time")
	}
}

func TestID_Binary(t *testing.T) {
	t.Parallel()
	id := ID{Dev: 1 << 40, Ino: 7, Mode: 0o100644, Size: 12, Mtime: -5, Ctime: 1_700_000_000_123_456_789}
	got, err := ReadBinary(bytes.NewReader(id.AppendBinary(nil)))
	if err != nil || got != id {
		t.Errorf("ReadBinary = %+v, %v; want %+v", got, err, id)
	}
}

func TestID_Trusted(t *testing.T) {
	t.Parallel()
	seen := time.Unix(1_000_000, 0)
	at := func(d time.Duration) int64 { return seen.Add(d).UnixNano() }
	for _, c := range []struct {
		mtime, ctime time.Duration
		margin       Margin
		want         bool
	}{
		{-DefaultMargin - 1, -DefaultMargin - 1, 0, true},
		{-DefaultMargin, -DefaultMargin - 1, 0, false}, // at the limit: not older
		{-DefaultMargin - 1, -time.Second, 0, false},
		{time.Minute, time.Minute, Margin(-time.Hour), true},
		{-30 * time.Minute, -30 * time.Minute, Margin(time.Hour), false},
	} {
		id := ID{Mtime: at(c.mtime), Ctime: at(c.ctime)}
		if got := id.Trusted(seen, c.margin); got != c.want {
			t.Errorf("mtime %v, ctime %v, margin %v: Trusted = %v", c.mtime, c.ctime, time.Duration(c.margin), got)
		}
	}
}

func TestReadStable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "content")
	s, ok := ReadStable(path)
	if !ok || string(s.Content()) != "content" {
		t.Fatalf("ReadStable = %q, %v", s.Content(), ok)
	}
	if !s.Holds("content") || s.Holds("other") {
		t.Error("Holds of an unchanged file")
	}
	if !s.Stamp().Unchanged(path) {
		t.Error("the stamp of an unchanged file")
	}

	time.Sleep(10 * time.Millisecond)
	writeFile(t, path, "content")
	if s.Holds("content") {
		t.Error("Holds after the file was rewritten")
	}

	if _, ok := ReadStable(dir); ok {
		t.Error("ReadStable of a directory")
	}
	if _, ok := ReadStable(filepath.Join(dir, "missing")); ok {
		t.Error("ReadStable of a missing file")
	}
	var zero Snapshot
	if zero.Holds("") {
		t.Error("the zero Snapshot holds something")
	}
}

func TestIsScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, c := range map[string]struct {
		content string
		script  bool
		err     bool
	}{
		"script": {"#!/bin/sh\n", true, false},
		"binary": {"\x7fELF", false, false},
		"short":  {"#", false, true},
	} {
		path := filepath.Join(dir, name)
		writeFile(t, path, c.content)
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		script, err := IsScript(f)
		_ = f.Close()
		if script != c.script || (err != nil) != c.err {
			t.Errorf("%s: IsScript = %v, %v", name, script, err)
		}
	}
}

// KeyHash prefixes strings with their lengths: parts that concatenate
// alike hash differently.
func TestKeyHash(t *testing.T) {
	t.Parallel()
	sum := func(parts ...string) string {
		k := NewKeyHash()
		for _, p := range parts {
			k.String(p)
		}

		return hex.EncodeToString(k.Sum(nil))
	}
	if sum("ab", "c") == sum("a", "bc") {
		t.Error("ab|c and a|bc hash alike")
	}
	if first, again := sum("x"), sum("x"); first != again {
		t.Error("not deterministic")
	}
}
