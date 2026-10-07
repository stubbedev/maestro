package resolver

import (
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/semver"
)

// FuzzDependencyHasher checks that dependencyHasher gives what the step by
// step port of calculateDependencyHash gives. Each input line is a link
// "key target constraint" of the section its first byte picks; a key other
// than the target lets two links share a target.
func FuzzDependencyHasher(f *testing.F) {
	for _, seed := range []string{
		"r a/b a/b ^1.0\nr c/d c/d ~2.0\nc e/f e/f <1.5",
		"r x a/b ^1.0\nr y a/b ^2.0\nr z 0/b *",
		"p 1 123 *\nr b/a b/a 1.0\nr a/b a/b 1.0",
		"r k1 z/z 1.0\nr k2 a/a 2.0\nr k3 z/z 3.0\nx q q/q 1.0",
		"r  a *",
	} {
		f.Add(seed)
	}
	constraints := map[string]semver.ConstraintInterface{}
	constraint := func(t *testing.T, s string) semver.ConstraintInterface {
		c, ok := constraints[s]
		if !ok {
			var err error
			if c, err = (semver.VersionParser{}).ParseConstraints(s); err != nil {
				c = semver.NewMatchAllConstraint()
			}
			constraints[s] = c
		}

		return c
	}
	f.Fuzz(func(t *testing.T, spec string) {
		var sections [4]pkg.LinksBuilder
		for line := range strings.SplitSeq(spec, "\n") {
			fields := strings.SplitN(line, " ", 4)
			if len(fields) != 4 || fields[0] == "" {
				continue
			}
			section := &sections[int(fields[0][0])%len(sections)]
			section.Set(fields[1], pkg.NewLink("root/pkg", fields[2], constraint(t, fields[3]), "requires", pkg.NullString{}))
		}
		p := pkg.NewPackage("root/pkg", "1.0.0.0", "1.0.0")
		p.SetRequires(sections[0].Build())
		p.SetConflicts(sections[1].Build())
		p.SetReplaces(sections[2].Build())
		p.SetProvides(sections[3].Build())

		constraintString := func(c semver.ConstraintInterface) string { return c.String() }
		want := string(appendDependencyHash([]byte("prefix"), p, constraintString))
		hasher := dependencyHasher{constraintString: constraintString}
		for range 2 { // the second time with scratch space left over
			if got := string(hasher.appendHash([]byte("prefix"), p)); got != want {
				t.Fatalf("dependencyHasher gives %q, calculateDependencyHash %q", got, want)
			}
		}
	})
}
