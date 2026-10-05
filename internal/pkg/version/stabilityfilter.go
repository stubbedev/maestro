// Ports src/Composer/Package/Version/StabilityFilter.php.

package version

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// IsPackageAcceptable ports StabilityFilter::isPackageAcceptable: whether a
// package with these names (the name and what it provides or replaces) and
// stability passes the stability flags (name => BasePackage::STABILITY_*)
// or, for names without a flag, the acceptable stabilities (stability name
// => value).
func IsPackageAcceptable(acceptableStabilities, stabilityFlags *php.Array, names []string, stability string) bool {
	for _, name := range names {
		// allow if package matches the package-specific stability flag
		if flag, ok := stabilityFlags.Get(name); ok && flag != nil {
			value, _ := pkg.StabilityValue(stability)
			if int64(value) <= php.ToInt(flag) {
				return true
			}
		} else if v, ok := acceptableStabilities.Get(stability); ok && v != nil {
			// allow if package matches the global stability requirement and has no exception
			return true
		}
	}

	return false
}
