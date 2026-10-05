package console

import (
	"reflect"
	"testing"
)

func TestGithubActionError_Emit(t *testing.T) {
	var got []string
	g := NewGithubActionError(func(m string) { got = append(got, m) })

	t.Setenv("GITHUB_ACTIONS", "")
	t.Setenv("COMPOSER_TESTS_ARE_RUNNING", "")
	g.Emit("ignored", "", 0)
	if got != nil {
		t.Fatalf("emitted outside GitHub Actions: %q", got)
	}

	t.Setenv("GITHUB_ACTIONS", "true")
	t.Setenv("COMPOSER_TESTS_ARE_RUNNING", "1")
	g.Emit("ignored", "", 0)
	if got != nil {
		t.Fatalf("emitted while tests are running: %q", got)
	}

	t.Setenv("COMPOSER_TESTS_ARE_RUNNING", "0")
	g.Emit("50% done\r\nnext", "a:b,c%\n.json", 12)
	g.Emit("msg", "file.json", 0)
	g.Emit("msg", "", 3)
	g.Emit("msg", "0", 3)

	want := []string{
		"::error file=a%3Ab%2Cc%25%0A.json,line=12::50%25 done%0D%0Anext",
		"::error file=file.json::msg",
		"::error ::msg",
		"::error ::msg",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("emitted %q, want %q", got, want)
	}
}
