// Ports src/Composer/Platform/Version.php.

package platform

import (
	"strconv"
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/semver"
)

var (
	opensslVersionRe = php.MustCompile(`/^(?<version>[0-9.]+)(?<patch>[a-z]{0,2})(?<suffix>(?:-?(?:dev|pre|alpha|beta|rc|fips)[\d]*)*)(?:-\w+)?(?: \(.+?\))?$/`)
	libjpegVersionRe = php.MustCompile(`/^(?<major>\d+)(?<minor>[a-z]*)$/`)
	zoneinfoRe       = php.MustCompile(`/^(?<year>\d{4})(?<revision>[a-z]*)$/`)
)

// ParseOpenssl ports Version::parseOpenssl: the normalizable version of an
// OpenSSL version string, and whether it is a FIPS build. ok is false for
// null.
func ParseOpenssl(opensslVersion string) (version string, isFips, ok bool) {
	m, err := opensslVersionRe.MatchStrictGroups(opensslVersion)
	if err != nil || m == nil {
		return "", false, false
	}

	ver, _ := m.Named("version")
	patchLetters, _ := m.Named("patch")
	suffix, _ := m.Named("suffix")

	// OpenSSL 1 used 1.2.3a style versioning, 3+ uses semver
	patch := ""
	if semver.VersionCompare(ver, "3.0.0") < 0 {
		patch = "." + strconv.Itoa(convertAlphaVersionToIntVersion(patchLetters))
	}

	isFips = strings.Contains(suffix, "fips")
	suffix = php.StrtrPairs("-"+php.LtrimSet(suffix, "-"), map[string]string{"-fips": "", "-pre": "-alpha"})

	return php.RtrimSet(ver+patch+suffix, "-"), isFips, true
}

// ParseLibjpeg ports Version::parseLibjpeg; ok is false for null.
func ParseLibjpeg(libjpegVersion string) (string, bool) {
	m, err := libjpegVersionRe.MatchStrictGroups(libjpegVersion)
	if err != nil || m == nil {
		return "", false
	}

	major, _ := m.Named("major")
	minor, _ := m.Named("minor")

	return major + "." + strconv.Itoa(convertAlphaVersionToIntVersion(minor)), true
}

// ParseZoneinfoVersion ports Version::parseZoneinfoVersion; ok is false
// for null.
func ParseZoneinfoVersion(zoneinfoVersion string) (string, bool) {
	m, err := zoneinfoRe.MatchStrictGroups(zoneinfoVersion)
	if err != nil || m == nil {
		return "", false
	}

	year, _ := m.Named("year")
	revision, _ := m.Named("revision")

	return year + "." + strconv.Itoa(convertAlphaVersionToIntVersion(revision)), true
}

// convertAlphaVersionToIntVersion ports Version's private helper:
// "" => 0, "a" => 1, "zg" => 33.
func convertAlphaVersionToIntVersion(alpha string) int {
	n := len(alpha) * (-'a' + 1)
	for i := range len(alpha) {
		n += int(alpha[i])
	}

	return n
}

// ConvertLibxpmVersionId ports Version::convertLibxpmVersionId.
func ConvertLibxpmVersionId(versionID int64) string {
	return convertVersionID(versionID, 100)
}

// ConvertOpenldapVersionId ports Version::convertOpenldapVersionId.
func ConvertOpenldapVersionId(versionID int64) string {
	return convertVersionID(versionID, 100)
}

// convertVersionID ports Version::convertVersionId: sprintf('%d.%d.%d',
// $id / base², (int) ($id / base) % base, $id % base), where the first
// division is a float division truncated by %d.
func convertVersionID(versionID, base int64) string {
	major := php.ToInt(float64(versionID) / float64(base*base))
	minor := php.ToInt(float64(versionID)/float64(base)) % base

	return strconv.FormatInt(major, 10) + "." + strconv.FormatInt(minor, 10) + "." + strconv.FormatInt(versionID%base, 10)
}
