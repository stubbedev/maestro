// Ports src/Composer/Util/PackageInfo.php. Composer keeps it in
// Composer\Util; it lives here because internal/util sits below the
// package classes.

package pkg

import "github.com/stubbedev/maestro/internal/php"

// GetViewSourceURL ports PackageInfo::getViewSourceUrl: support.source,
// else the source URL.
func GetViewSourceURL(p PackageInterface) NullString {
	if c, ok := p.(CompletePackageInterface); ok {
		if source, ok := c.Support().Get("source"); ok && source != nil && source != "" {
			return Str(php.ToString(source))
		}
	}

	return p.SourceURL()
}

// GetViewSourceOrHomepageURL ports PackageInfo::getViewSourceOrHomepageUrl.
func GetViewSourceOrHomepageURL(p PackageInterface) NullString {
	url := GetViewSourceURL(p)
	if !url.Valid {
		if c, ok := p.(CompletePackageInterface); ok {
			url = c.Homepage()
		}
	}

	if url == Str("") {
		return NullString{}
	}

	return url
}
