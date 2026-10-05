// Package testutil holds helpers shared by the tests of several packages.
// It is imported only from _test.go files.
package testutil

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// ReadGoldenFile reads a golden file written by an oracle script under
// tools/oracle, transparently decompressing it when its name ends in ".gz"
// (goldens over 1 MB are committed gzipped, docs/PORTING.md).
func ReadGoldenFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || !strings.HasSuffix(path, ".gz") {
		return data, err
	}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out, err := io.ReadAll(zr)
	if cerr := zr.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// ReadGolden is ReadGoldenFile, failing the test on error.
func ReadGolden(tb testing.TB, path string) []byte {
	tb.Helper()
	data, err := ReadGoldenFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// LoadJSONGolden decodes the (possibly gzipped) JSON golden at path into v
// with encoding/json, failing the test on error.
func LoadJSONGolden(tb testing.TB, path string, v any) {
	tb.Helper()
	if err := json.Unmarshal(ReadGolden(tb, path), v); err != nil {
		tb.Fatalf("%s: %v", path, err)
	}
}
