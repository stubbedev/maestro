// Ports src/Composer/Util/ComposerMirror.php.

package util

import (
	"crypto/md5" //nolint:gosec // Composer names mirror paths by md5, not for security.
	"encoding/hex"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
)

var (
	mirrorReference = php.MustCompile(`{^([a-f0-9]*|%reference%)$}`)
	mirrorGitHub    = php.MustCompile(`#^(?:(?:https?|git)://github\.com/|git@github\.com:)([^/]+)/(.+?)(?:\.git)?$#`)
	mirrorBitbucket = php.MustCompile(`#^https://bitbucket\.org/([^/]+)/(.+?)(?:\.git)?/?$#`)
)

// ComposerMirrorProcessURL ports ComposerMirror::processUrl. reference and
// typ are nil for PHP's null; prettyVersion is nil when not given.
func ComposerMirrorProcessURL(mirrorURL, packageName, version string, reference, typ, prettyVersion *string) string {
	ref := ""
	if reference != nil {
		ref = *reference
		// if ($reference): "0" is falsy too.
		if phpTruthy(ref) {
			// [a-f0-9]* is possessive before $: Preg::isMatch cannot throw.
			if isRef, _ := mirrorReference.IsMatch(ref); !isRef {
				ref = md5Hex(ref)
			}
		}
	}

	if strings.IndexByte(version, '/') >= 0 {
		version = md5Hex(version)
	}

	// str_replace with arrays replaces each pair in turn over the whole
	// string.
	url := strings.ReplaceAll(mirrorURL, "%package%", packageName)
	url = strings.ReplaceAll(url, "%version%", version)
	url = strings.ReplaceAll(url, "%reference%", ref)
	url = strings.ReplaceAll(url, "%type%", deref(typ))

	if prettyVersion != nil {
		url = strings.ReplaceAll(url, "%prettyVersion%", *prettyVersion)
	}

	return url
}

// ComposerMirrorProcessGitURL ports ComposerMirror::processGitUrl. typ is
// nil for PHP's null.
func ComposerMirrorProcessGitURL(mirrorURL, packageName, url string, typ *string) string {
	// Preg::isMatch throws a PcreException on these patterns only for URLs
	// of about a megabyte (the backtrack limit). Package::getSourceUrls and
	// getDistUrls, which call this, have no error result here, so such a
	// URL reads as no match (docs/PORTING.md "Regular expressions").
	if m, _ := mirrorGitHub.Match(url); m != nil {
		url = "gh-" + m.Get(1) + "/" + m.Get(2)
	} else if m, _ := mirrorBitbucket.Match(url); m != nil {
		url = "bb-" + m.Get(1) + "/" + m.Get(2)
	} else {
		// Preg::replace('{[^a-z0-9_.-]}i', '-', trim($url, '/')), byte-wise.
		b := []byte(strings.Trim(url, "/"))
		for i, c := range b {
			if !isASCIIAlnum(c) && c != '_' && c != '.' && c != '-' {
				b[i] = '-'
			}
		}

		url = string(b)
	}

	result := strings.ReplaceAll(mirrorURL, "%package%", packageName)
	result = strings.ReplaceAll(result, "%normalizedUrl%", url)

	return strings.ReplaceAll(result, "%type%", deref(typ))
}

// ComposerMirrorProcessHgURL ports ComposerMirror::processHgUrl.
func ComposerMirrorProcessHgURL(mirrorURL, packageName, url, typ string) string {
	return ComposerMirrorProcessGitURL(mirrorURL, packageName, url, &typ)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // See the import.

	return hex.EncodeToString(sum[:])
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
