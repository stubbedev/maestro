package pkg_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// FuzzIsPlatformPackage checks the hand-written matcher against
// PlatformRepository::PLATFORM_PACKAGE_REGEX.
func FuzzIsPlatformPackage(f *testing.F) {
	re := php.MustCompile(`{^(?:php(?:-64bit|-ipv6|-zts|-debug)?|hhvm|(?:ext|lib)-[a-z0-9](?:[_.-]?[a-z0-9]+)*|composer(?:-(?:plugin|runtime)-api)?)$}iD`)

	for _, s := range []string{"php", "PHP-zts", "ext-a.b", "ext-", "lib-a--b", "composer-plugin-api", "hhvm\n", "ext-ſ", "compoſer", "Ext-A_B-c"} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		want, err := re.IsMatch(s)
		if err != nil {
			t.Skip()
		}

		if got := pkg.IsPlatformPackage(s); got != want {
			t.Errorf("IsPlatformPackage(%q) = %v, want %v", s, got, want)
		}
	})
}
