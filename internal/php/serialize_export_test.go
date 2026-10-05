package php

import (
	"math"
	"testing"
)

func TestSerialize(t *testing.T) {
	obj := NewObject()
	obj.Set("0", "x")
	for _, v := range []any{
		nil, true, false, int64(-5), 0.1, math.Inf(1), "a\"b\x00",
		ArrayOf("symlink", true, "relative", false, 5, ListOf(1.5, "x"), "o", obj),
		NewArray(),
	} {
		if got, want := Serialize(v), phpSerialize(v); got != want {
			t.Errorf("Serialize(%v) = %q, want %q", v, got, want)
		}
	}
	if got := Serialize(ArrayOf("reference", "config", "relative", true)); got != `a:2:{s:9:"reference";s:6:"config";s:8:"relative";b:1;}` {
		t.Error(got)
	}
}
