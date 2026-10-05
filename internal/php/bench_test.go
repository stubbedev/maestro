package php

import (
	"strconv"
	"strings"
	"testing"
)

var benchKeys = func() []string {
	ks := make([]string, 64)
	for i := range ks {
		ks[i] = "key-" + strconv.Itoa(i)
	}
	return ks
}()

func BenchmarkArraySetSmall(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewArrayCap(8)
		for _, k := range benchKeys[:8] {
			a.SetKey(StrKey(k), int64(1))
		}
	}
}

func BenchmarkArraySetLarge(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewArrayCap(len(benchKeys))
		for _, k := range benchKeys {
			a.SetKey(StrKey(k), int64(1))
		}
	}
}

func BenchmarkArrayGet(b *testing.B) {
	a := NewArray()
	for _, k := range benchKeys {
		a.Set(k, true)
	}
	keys := make([]Key, len(benchKeys))
	for i, k := range benchKeys {
		keys[i] = StrKey(k)
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, k := range keys {
			if _, ok := a.GetKey(k); !ok {
				b.Fatal(k)
			}
		}
	}
}

func BenchmarkArrayAppendList(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		a := NewArrayCap(64)
		for i := range 64 {
			a.Append(int64(i))
		}
	}
}

func BenchmarkArrayIterate(b *testing.B) {
	a := NewArray()
	for _, k := range benchKeys {
		a.Set(k, true)
	}
	b.ReportAllocs()
	for b.Loop() {
		n := 0
		for range a.All() {
			n++
		}
	}
}

// benchComposerJSON is a composer.json-like document.
var benchComposerJSON = func() string {
	var sb strings.Builder
	sb.WriteString(`{"name":"vendor/package","description":"A package with a description that is long enough to matter, and some unicode: é€😀","type":"library","license":"MIT","require":{`)
	for i := range 40 {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(`"vendor` + strconv.Itoa(i) + `/package-name":"^1.` + strconv.Itoa(i) + ` || ^2.0"`)
	}
	sb.WriteString(`},"autoload":{"psr-4":{"Vendor\\Package\\":"src/"},"files":["src/functions.php"]},"extra":{"branch-alias":{"dev-main":"2.x-dev"},"n":[1,2.5,true,null]}}`)
	return sb.String()
}()

func BenchmarkJSONDecode(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(int64(len(benchComposerJSON)))
	for b.Loop() {
		if _, err := JSONDecode(benchComposerJSON, true); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONEncodePretty(b *testing.B) {
	v, err := JSONDecode(benchComposerJSON, true)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := JSONEncode(v, JSONPrettyPrint|JSONUnescapedSlashes|JSONUnescapedUnicode); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONEncodeEscaped(b *testing.B) {
	v, err := JSONDecode(benchComposerJSON, false)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := JSONEncode(v, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkVarExport(b *testing.B) {
	v, err := JSONDecode(benchComposerJSON, true)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		_ = VarExport(v)
	}
}

func BenchmarkRegexpIsMatchVersion(b *testing.B) {
	re := MustCompile(`{^v?(\d{1,5}+)(\.\d++)?(\.\d++)?(\.\d++)?[._-]?(?:(stable|beta|b|RC|alpha|a|patch|pl|p)((?:[.-]?\d+)*+)?)?([.-]?dev)?$}i`)
	b.ReportAllocs()
	for b.Loop() {
		if ok, _ := re.IsMatch("v1.2.3-beta.4"); !ok {
			b.Fatal("no match")
		}
	}
}

func BenchmarkRegexpMatchAllLarge(b *testing.B) {
	subject := strings.Repeat("<?php\nnamespace Foo\\Bar;\n\nfinal class Baz extends Qux implements Quux\n{\n    public function x(): void {}\n}\n", 50)
	re := MustCompile(`{\b(?:class|interface|trait|enum)\s+([a-zA-Z_\x7f-\xff][a-zA-Z0-9_\x7f-\xff]*)}i`)
	b.ReportAllocs()
	b.SetBytes(int64(len(subject)))
	for b.Loop() {
		ms, err := re.MatchAll(subject)
		if err != nil || len(ms) != 50 {
			b.Fatal(len(ms), err)
		}
	}
}

func BenchmarkRegexpReplace(b *testing.B) {
	re := MustCompile(`{[^a-z0-9._]}i`)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := re.Replace("https://repo.packagist.org/p2/vendor/package~dev.json", "-", -1); err != nil {
			b.Fatal(err)
		}
	}
}
