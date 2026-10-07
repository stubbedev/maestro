package resolver

import (
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

// TestCreatePool_AllowedTypes checks PoolBuilder's ?array $allowedTypes:
// null loads packages of every type, [] (even a nil slice) loads
// none, and a list only those types.
func TestCreatePool_AllowedTypes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allowed php.Nullable[[]string]
		want    []string
	}{
		{"null", php.Null[[]string](), []string{"a/lib", "a/plugin"}},
		{"empty", php.Some([]string{}), nil},
		{"nil slice", php.Some[[]string](nil), nil},
		{"plugins", php.Some([]string{"composer-plugin"}), []string{"a/plugin"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lib := getPackage(t, "a/lib", "1.0.0")
			plugin := getPackage(t, "a/plugin", "1.0.0")
			plugin.SetType("composer-plugin")
			set := newRepositorySet(t, "stable")
			addRepository(t, set, newArrayRepository(t, lib, plugin))
			request := NewRequest(nil)
			requireName(t, request, "a/lib", matchAll(""))
			requireName(t, request, "a/plugin", matchAll(""))

			pool, err := CreatePool(set, request, nullIO, CreatePoolOptions{AllowedTypes: tc.allowed})
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, p := range pool.Packages() {
				got = append(got, p.Name())
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("pool %v, want %v", got, tc.want)
			}
		})
	}
}
