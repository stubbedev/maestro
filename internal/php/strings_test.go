package php

import "testing"

func TestStrncasecmpStripos(t *testing.T) {
	for _, c := range []struct {
		a, b string
		n    int
		want int
	}{
		{"FILE://x", "file://", 7, 0},
		{"fi", "file://", 7, -1},
		{"abd", "ABC", 2, 0},
		{"abd", "ABC", 3, 1},
		{"", "", 0, 0},
	} {
		if got := Strncasecmp(c.a, c.b, c.n); got != c.want {
			t.Errorf("Strncasecmp(%q, %q, %d) = %d, want %d", c.a, c.b, c.n, got, c.want)
		}
	}

	for _, c := range []struct {
		h, n string
		want int
	}{
		{"X-RateLimit-Limit: 5", "x-ratelimit-", 0},
		{"abcABC", "Bc", 1},
		{"abc", "abcd", -1},
		{"abc", "", 0},
	} {
		if got := Stripos(c.h, c.n); got != c.want {
			t.Errorf("Stripos(%q, %q) = %d, want %d", c.h, c.n, got, c.want)
		}
	}
}

func TestURLFunctions(t *testing.T) {
	if got := Rawurlencode("a b~/é"); got != "a%20b~%2F%C3%A9" {
		t.Errorf("Rawurlencode = %q", got)
	}
	if got := Urlencode("a b~"); got != "a+b%7E" {
		t.Errorf("Urlencode = %q", got)
	}
	if got := Rawurldecode("a%20b+%zz%4"); got != "a b+%zz%4" {
		t.Errorf("Rawurldecode = %q", got)
	}
	if got := HTTPBuildQuery("a b", "c&d", "e", ""); got != "a+b=c%26d&e=" {
		t.Errorf("HTTPBuildQuery = %q", got)
	}
	if got := Date("Y-m-d H:i:s", 0); got != "1970-01-01 00:00:00" {
		t.Errorf("Date = %q", got)
	}
}
