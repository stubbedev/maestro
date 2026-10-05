package php

import "testing"

// TestBase64Decode checks base64_decode against PHP 8 (cases generated
// with php -r, base64_decode($s, $strict)).
func TestBase64Decode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in     string
		strict bool
		want   string
		ok     bool
	}{
		{"", false, "", true},
		{"", true, "", true},
		{"Zm9v", false, "foo", true},
		{"Zm9v", true, "foo", true},
		{"Zm9vYg==", false, "foob", true},
		{"Zm9vYg==", true, "foob", true},
		{"Zm9vYg=", false, "foob", true},
		{"Zm9vYg=", true, "", false},
		{"Zm9vYg", false, "foob", true},
		{"Zm9vYg", true, "foob", true},
		{"Zm9vY", false, "foo", true},
		{"Zm9vY", true, "", false},
		{"Z", false, "", true},
		{"Z", true, "", false},
		{"Zm9v\nYmFy", false, "foobar", true},
		{"Zm9v\nYmFy", true, "foobar", true},
		{"Zm9v YmFy", false, "foobar", true},
		{"Zm9v YmFy", true, "foobar", true},
		{"Zm9v*YmFy", false, "foobar", true},
		{"Zm9v*YmFy", true, "", false},
		{"Zm9vYg==Zg", false, "foob\u0006`", true},
		{"Zm9vYg==Zg", true, "", false},
		{"Zm9vYg== ", false, "foob", true},
		{"Zm9vYg== ", true, "foob", true},
		{"Zm9vYg===", false, "foob", true},
		{"Zm9vYg===", true, "", false},
		{"=Zm9v", false, "foo", true},
		{"=Zm9v", true, "", false},
		{"Zm=9v", false, "foo", true},
		{"Zm=9v", true, "", false},
		{"!!!!", false, "", true},
		{"!!!!", true, "", false},
		{"eyJhIjoxfQ==", false, "{\"a\":1}", true},
		{"eyJhIjoxfQ==", true, "{\"a\":1}", true},
		{"Zg==\r\n", false, "f", true},
		{"Zg==\r\n", true, "f", true},
		{"Zm9vYmFy\t", false, "foobar", true},
		{"Zm9vYmFy\t", true, "foobar", true},
		{"Zm9v\u000bYmFy", false, "foobar", true},
		{"Zm9v\u000bYmFy", true, "", false},
	}

	for _, c := range cases {
		got, ok := Base64Decode(c.in, c.strict)
		if got != c.want || ok != c.ok {
			t.Errorf("Base64Decode(%q, %v) = %q, %v; want %q, %v", c.in, c.strict, got, ok, c.want, c.ok)
		}
	}
}
