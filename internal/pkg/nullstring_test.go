package pkg

import "testing"

// A ?T crosses to PHP as a plain string or null, whatever its string
// type: the PHP values hold no Go named types.
func TestNull_Value(t *testing.T) {
	if v, ok := Some(FromDist).Value().(string); !ok || v != "dist" {
		t.Errorf("Some(FromDist).Value() = %#v, want the string dist", Some(FromDist).Value())
	}
	if v := (Null[InstallationSource]{}).Value(); v != nil {
		t.Errorf("null.Value() = %#v, want nil", v)
	}
}

// Is is ===: a null is no value, the zero one included.
func TestNull_Is(t *testing.T) {
	if (Null[InstallationSource]{}).Is("") {
		t.Error(`null is ""`)
	}
	if !Some(FromSource).Is(FromSource) || Some(FromSource).Is(FromDist) {
		t.Error("Is does not compare the value")
	}
	if got := NullAs[InstallationSource](Str("source")); got != Some(FromSource) {
		t.Errorf("NullAs = %v", got)
	}
	if got := NullAs[InstallationSource](NullString{}); got.Valid {
		t.Errorf("NullAs(null) = %v", got)
	}
}
