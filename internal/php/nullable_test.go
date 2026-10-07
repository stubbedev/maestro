package php

import "testing"

func TestNullable(t *testing.T) {
	var zero Nullable[[]string]
	if !zero.IsNull() {
		t.Error("the zero value is not null")
	}
	if !Null[[]string]().IsNull() {
		t.Error("Null() is not null")
	}
	if got := Null[[]string]().OrElse([]string{"d"}); len(got) != 1 {
		t.Errorf("null ?? [d] = %v", got)
	}

	// [] and even a nil slice wrapped in Some are not null
	for _, v := range [][]string{{}, nil} {
		n := Some(v)
		if n.IsNull() {
			t.Errorf("Some(%#v) is null", v)
		}
		if got, ok := n.Get(); !ok || len(got) != 0 {
			t.Errorf("Some(%#v).Get() = %v, %v", v, got, ok)
		}
		if got := n.OrElse([]string{"d"}); len(got) != 0 {
			t.Errorf("Some(%#v) ?? [d] = %v", v, got)
		}
	}
}
