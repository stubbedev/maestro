// Ports the checks of bin/composer that abort on the PHP Composer runs on.

package platform

import (
	"github.com/stubbedev/maestro/internal/semver"
)

// UnsupportedPHPError is one of bin/composer's aborts: Composer echoes
// Message (with PHP_EOL) to standard output and exits with code 1.
type UnsupportedPHPError struct {
	Message string
}

func (e *UnsupportedPHPError) Error() string { return e.Message }

// CheckBinComposer ports the checks bin/composer makes before running
// Composer: the PHP version, HHVM 4+, and iconv or mbstring. s is the
// ComposerView (the HHVM and extension checks follow the xdebug restart).
func (s *Snapshot) CheckBinComposer() error {
	if s.VersionID < 70205 {
		return &UnsupportedPHPError{Message: "Composer 2.3.0 dropped support for PHP <7.2.5 and you are running " + s.Version + ", please upgrade PHP or use Composer 2.2 LTS via \"composer self-update --2.2\". Aborting."}
	}

	if v, ok := s.Constant("HHVM_VERSION"); ok {
		if version, _ := v.(string); semver.VersionCompare(version, "4.0") >= 0 {
			return &UnsupportedPHPError{Message: "HHVM 4.0 has dropped support for Composer, please use PHP instead. Aborting."}
		}
	}

	eol, _ := s.Constant("PHP_EOL")
	nl, _ := eol.(string)

	r := NewRuntime(s)
	if _, iconv := r.extension("iconv"); !iconv {
		if _, mbstring := r.extension("mbstring"); !mbstring {
			return &UnsupportedPHPError{Message: "The iconv OR mbstring extension is required and both are missing." + nl + "Install either of them or recompile php without --disable-iconv." + nl + "Aborting."}
		}
	}

	return nil
}
