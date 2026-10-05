package php

import (
	"encoding/base64"
	"errors"
	"testing"
)

// Tests against the goldens of tools/oracle/php/funcs.php.

// funcsString decodes a golden string, plain or {"base64": ...}.
func funcsString(t *testing.T, v any) string {
	t.Helper()
	switch v := v.(type) {
	case string:
		return v
	case map[string]any:
		if b64, ok := v["base64"].(string); ok {
			b, err := base64.StdEncoding.DecodeString(b64)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
	}
	t.Fatalf("not a golden string: %#v", v)
	return ""
}

func TestOracleFuncs(t *testing.T) {
	for _, c := range loadOracle(t, "funcs") {
		fn := c["fn"].(string)
		raw := c["args"].([]any)
		args := make([]string, len(raw))
		for i, a := range raw {
			args[i] = funcsString(t, a)
		}

		var got string
		var err error
		switch fn {
		case "strip_tags":
			got = StripTags(args[0])
		case "levenshtein":
			got = phpSerialize(int64(Levenshtein(args[0], args[1])))
		case "stripcslashes":
			got = Stripcslashes(args[0])
		case "escapeshellarg":
			got, err = Escapeshellarg(args[0])
		case "basename":
			got = Basename(args[0], args[1])
		case "sprintf":
			list := phpUnserialize(t, args[1]).(*Array).Values()
			got, err = Sprintf(args[0], list...)
		case "==":
			got = phpSerialize(StringsLooseEqual(args[0], args[1]))
			if want := LooseEquals(args[0], args[1]); StringsLooseEqual(args[0], args[1]) != want {
				t.Errorf("StringsLooseEqual(%q, %q) disagrees with LooseEquals", args[0], args[1])
			}
		case "+":
			var r any
			r, err = Add(phpUnserialize(t, args[0]), phpUnserialize(t, args[1]))
			if err == nil {
				got = phpSerialize(r)
			}
		default:
			t.Fatalf("unknown function %s", fn)
		}

		if m, ok := c["result"].(map[string]any); ok && m["error"] != nil {
			e, ok := errors.AsType[*EngineError](err)
			if !ok || e.Message != m["error"] || e.Class != m["class"] {
				t.Errorf("%s%q: got %q, %v; want %s: %s", fn, args, got, err, m["class"], m["error"])
			}
			continue
		}
		want := funcsString(t, c["result"])
		if err != nil || got != want {
			t.Errorf("%s%q: got %q, %v; want %q", fn, args, got, err, want)
		}
	}
}
