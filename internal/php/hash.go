// Ports PHP's hash helpers: md5(), sha1(), sha1_file() and
// bin2hex(random_bytes()).

package php

import (
	"crypto/md5" //nolint:gosec // PHP's md5(): fingerprints, not security
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // PHP's sha1(): fingerprints, not security
	"encoding/hex"
	"io"
	"os"
)

// Md5 ports md5($s).
func Md5(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // see the import

	return hex.EncodeToString(sum[:])
}

// Sha1 ports sha1($s).
func Sha1(s string) string {
	sum := sha1.Sum([]byte(s)) //nolint:gosec // see the import

	return hex.EncodeToString(sum[:])
}

// Sha1File ports sha1_file($path) (and hash_file('sha1', $path)).
func Sha1File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New() //nolint:gosec // see the import
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// RandomHex is bin2hex(random_bytes($n)).
func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)

	return hex.EncodeToString(b)
}
