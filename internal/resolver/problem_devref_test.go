package resolver

import "testing"

// Problem.php matches `{^dev-.*#.*}`: the # must come before any newline,
// since `.` does not match one.
func TestDevRefRegex(t *testing.T) {
	cases := map[string]bool{
		"dev-main#abc123":   true,
		"dev-main#":         true,
		"dev-main":          false,
		"main#abc":          false,
		"dev-main\n#abc123": false,
		"dev-a#b\nc":        true,
	}

	for in, want := range cases {
		got, err := devRefRegex.IsMatch(in)
		if err != nil {
			t.Fatal(err)
		}

		if got != want {
			t.Errorf("devRefRegex.IsMatch(%q) = %v, want %v", in, got, want)
		}
	}
}
