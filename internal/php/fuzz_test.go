package php

import (
	"reflect"
	"testing"
	"unicode/utf8"
)

// FuzzJSONRoundTrip checks the json_decode/json_encode invariants: what
// decodes re-encodes to valid JSON, and a second round trip is stable (the
// first may not be: -0.0 encodes as -0, which decodes as the int 0).
func FuzzJSONRoundTrip(f *testing.F) {
	for _, s := range []string{
		`null`, `[1,2.5,-0.0,1e400,"a\u00e9\ud83d\ude00"]`, `{"a":{"b":[]},"0":1,"":{}}`, `"\u0000\/"`, `12345678901234567890`,
		`[[[[]]]]`, `{"a":1,"a":2}`, `[1.0,0.1,1e-7,123456789012345678]`, "\"\xff\"", `{"x":"<>&'\""}`,
	} {
		f.Add(s, true)
		f.Add(s, false)
	}
	f.Fuzz(func(t *testing.T, s string, assoc bool) {
		v, err := JSONDecode(s, assoc)
		if err != nil {
			return
		}
		for _, flags := range []JSONFlag{0, JSONPrettyPrint | JSONUnescapedSlashes | JSONUnescapedUnicode, JSONPreserveZeroFraction} {
			enc, err := JSONEncode(v, flags)
			if err != nil {
				// Only floats too large for JSON fail: 1e400 decodes to INF.
				continue
			}
			if !utf8.ValidString(enc) {
				t.Fatalf("invalid UTF-8 output %q", enc)
			}
			v2, err := JSONDecode(enc, assoc)
			if err != nil {
				t.Fatalf("re-decode of %q: %v", enc, err)
			}
			enc2, err := JSONEncode(v2, flags)
			if err != nil {
				t.Fatalf("re-encode of %q: %v", enc, err)
			}
			v3, err := JSONDecode(enc2, assoc)
			if err != nil {
				t.Fatalf("re-decode of %q: %v", enc2, err)
			}
			if enc3, err := JSONEncode(v3, flags); err != nil || enc3 != enc2 {
				t.Fatalf("unstable: %q -> %q -> %q (%v)", enc, enc2, enc3, err)
			}
		}
	})
}

// FuzzJSONDecodeSpans checks that JSONDecodeSpans decodes what
// json_decode decodes, and that each value's span decodes to the value,
// for the arrays and objects of every level.
func FuzzJSONDecodeSpans(f *testing.F) {
	for _, s := range []string{
		`[1,2]`, ` { "a" : [ {"b":1} , "x\"y" , -1.5e3 ] , "c":{"d":null} } `, `{"a":1,"a":2}`, `{"p":{"x":[1],"x":[2]}}`,
		`{"0":[],"1":{},"":true}`, `[[[[]]]]`, `{"packages":{"a\/b":[{"name":"a\/b"},{"version":"1.0"}]}}`,
	} {
		for level := range 3 {
			f.Add(s, uint8(level))
		}
	}
	f.Fuzz(func(t *testing.T, s string, level uint8) {
		want, err := JSONDecode(s, true)
		v, spans, errSpans := JSONDecodeSpans(s, JSONDefaultDepth, int(level%4))
		if (err == nil) != (errSpans == nil) {
			t.Fatalf("errors differ: %v, %v", err, errSpans)
		}
		if err != nil {
			return
		}
		if !reflect.DeepEqual(v, want) {
			t.Fatal("decodes to another value")
		}
		var walk func(v any, at int)
		walk = func(v any, at int) {
			a, ok := v.(*Array)
			if !ok {
				return
			}
			if at < int(level%4) {
				for _, e := range a.Values() {
					walk(e, at+1)
				}

				return
			}
			sp, ok := spans[a]
			if !ok {
				return
			}
			if len(sp) != a.Len() {
				t.Fatalf("%d spans for %d values", len(sp), a.Len())
			}
			for i, e := range a.Values() {
				got, err := JSONDecode(s[sp[i].Start:sp[i].End], true)
				if err != nil || !reflect.DeepEqual(got, e) {
					t.Fatalf("span %q decodes to another value (%v)", s[sp[i].Start:sp[i].End], err)
				}
			}
		}
		walk(v, 0)
	})
}

// FuzzRegexp checks that any pattern either fails to compile or matches
// without panicking or running away, and that the preg operations agree
// with each other.
func FuzzRegexp(f *testing.F) {
	for _, p := range []string{
		`/a+b/`, `{^(?<x>\d+)(?:\.(?&x))*$}`, `/(?<=a|bc)\b\w+/u`, `/(a|ab)(c|bcd)(d*)/i`, `#\Q.*\E(?(1)a|b)#x`,
		`/[[:alpha:]\p{L}\d-]+/u`, `/(?>a+)++\K./s`, `/^$/m`,
	} {
		f.Add(p, "abc ab12.34 bcd é")
	}
	f.Fuzz(func(t *testing.T, pattern, subject string) {
		re, err := Compile(pattern)
		if err != nil {
			return
		}
		ms, err := re.MatchAll(subject)
		if err != nil {
			return
		}
		res, n, err := re.Replace(subject, "<$0>", -1)
		if err != nil || n != len(ms) {
			t.Fatalf("%q on %q: %d matches, %d replacements (%v)", pattern, subject, len(ms), n, err)
		}
		if n == 0 && res != subject {
			t.Fatalf("no replacement changed the subject")
		}
		for _, m := range ms {
			for g := range m.Groups() {
				if o := m.Offset(g); o >= 0 && (o > len(subject) || m.offs[2*g+1] < o && g > 0) {
					t.Fatalf("group %d out of range: %v", g, m.offs)
				}
			}
		}
	})
}
