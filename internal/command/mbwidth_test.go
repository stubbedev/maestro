package command

import "testing"

// Expected values recorded from PHP 8.4's mb_strimwidth/mb_strwidth.
func TestMbStrimwidth(t *testing.T) {
	cases := []struct {
		s     string
		w     int
		want  string
		width int
	}{
		{"hello world", 0, "...", 11},
		{"hello world", 3, "...", 11},
		{"hello world", 4, "h...", 11},
		{"hello world", 10, "hello w...", 11},
		{"日本語のテキストです", 4, "...", 20},
		{"日本語のテキストです", 5, "日...", 20},
		{"日本語のテキストです", 10, "日本語...", 20},
		{"aé日b", 4, "a...", 5},
		{"aé日b", 5, "aé日b", 5},
		{"ab", 1, "...", 2},
		{"a", 0, "...", 1},
		{"a", 1, "a", 1},
	}
	for _, c := range cases {
		if got := mbStrimwidth(c.s, c.w, "..."); got != c.want {
			t.Errorf("mbStrimwidth(%q, %d) = %q, want %q", c.s, c.w, got, c.want)
		}
		if got := mbStrwidth(c.s); got != c.width {
			t.Errorf("mbStrwidth(%q) = %d, want %d", c.s, got, c.width)
		}
	}
}
