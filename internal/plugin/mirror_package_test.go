package plugin

import (
	"testing"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
)

// field is a field of changes (nil for none).
func field(changes *php.Array, name string) any {
	if changes == nil {
		return nil
	}
	v, _ := changes.Get(name)

	return v
}

// A package whose id alone changed since PHP got it crosses as its id;
// any other change, or one PHP never got, as the whole snapshot.
func TestPackageMirror_Changes(t *testing.T) {
	p := pkg.NewCompletePackage("acme/lib", "1.0.0.0", "1.0.0")
	m := &packageMirror{p: p}

	if _, ok := m.MirrorChanges(p.Rev()); ok {
		t.Error("changes of a package PHP never got")
	}

	if _, err := m.MirrorSnapshot(); err != nil {
		t.Fatal(err)
	}
	sent := p.Rev()
	p.SetID(7)
	changes, ok := m.MirrorChanges(sent)
	if !ok || changes.Len() != 1 || php.ToNativeInt(field(changes, "id")) != 7 {
		t.Errorf("after SetID: %v, %v", changes, ok)
	}

	// without the name, which would end what PHP waits for
	m.lazy.Store(true)
	sent = p.Rev()
	p.SetID(8)
	if changes, ok := m.MirrorChanges(sent); !ok || changes.Len() != 1 {
		t.Errorf("lazy, after SetID: %v, %v", changes, ok)
	}
	m.lazy.Store(false)

	sent = p.Rev()
	p.SetID(9)
	p.SetExtra(php.ArrayOf("a", int64(1)))
	if _, ok := m.MirrorChanges(sent); ok {
		t.Error("changes after SetExtra")
	}

	// changes asked since a revision PHP did not get
	if _, err := m.MirrorSnapshot(); err != nil {
		t.Fatal(err)
	}
	p.SetID(10)
	if _, ok := m.MirrorChanges(sent); ok {
		t.Error("changes since an older revision")
	}

	// an alias: its own id or its package's
	a := pkg.NewCompleteAliasPackage(p, "2.0.0.0", "2.0.0")
	am := &packageMirror{r: New(Options{}), p: a}
	if _, err := am.MirrorSnapshot(); err != nil {
		t.Fatal(err)
	}
	sent = a.Rev()
	p.SetID(11)
	a.SetID(12)
	if changes, ok := am.MirrorChanges(sent); !ok || php.ToNativeInt(field(changes, "id")) != 12 {
		t.Errorf("alias after SetID: %v, %v", changes, ok)
	}
}
