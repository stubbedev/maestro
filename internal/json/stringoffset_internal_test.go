package json

import "testing"

// stringOffset reads PHP string offsets: negative ones count from the end,
// and any offset outside the string, int's range included, is unset.
func TestStringOffset(t *testing.T) {
	tests := []struct {
		key  any
		want int
		ok   bool
	}{
		{0, 0, true},
		{2, 2, true},
		{3, 0, false},
		{-1, 2, true},
		{-3, 0, true},
		{-4, 0, false},
		{int64(1), 1, true},
		{int64(-2), 1, true},
		{int64(1) << 40, 0, false},
		{-(int64(1) << 40), 0, false},
		{"1", 1, true},
		{"-1", 2, true},
		{"01", 0, false},
		{"1.0", 0, false},
		{"9223372036854775807", 0, false},
		{"-9223372036854775808", 0, false},
		{"9223372036854775808", 0, false},
		{1.0, 0, false},
		{nil, 0, false},
	}

	for _, tt := range tests {
		got, ok := stringOffset("abc", tt.key)
		if got != tt.want || ok != tt.ok {
			t.Errorf("stringOffset(%q, %#v) = %d, %t, want %d, %t", "abc", tt.key, got, ok, tt.want, tt.ok)
		}
	}
}
