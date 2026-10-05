// Ports tests/Composer/Test/Util/SvnTest.php.

package vcs

import (
	"errors"
	"slices"
	"testing"

	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/processmock"
)

func credentialArgs(t *testing.T, svn *Svn) []string {
	t.Helper()

	args, err := svn.getCredentialArgs()
	if err != nil {
		t.Fatal(err)
	}

	return args
}

func TestSvn_Credentials(t *testing.T) {
	for _, tc := range []struct {
		url    string
		expect []string
	}{
		{"http://till:test@svn.example.org/", []string{"--username", "till", "--password", "test"}},
		{"http://svn.apache.org/", []string{}},
		{"svn://johndoe@example.org", []string{"--username", "johndoe", "--password", ""}},
	} {
		svn := NewSvn(tc.url, io.NewNullIO(), newFakeConfig(nil), nil)
		if got := credentialArgs(t, svn); !slices.Equal(got, tc.expect) {
			t.Errorf("%s: got %q, want %q", tc.url, got, tc.expect)
		}
	}
}

func TestSvn_InteractiveString(t *testing.T) {
	url := "http://svn.example.org"

	svn := NewSvn(url, io.NewNullIO(), newFakeConfig(nil), nil)

	got, err := svn.getCommand([]string{"svn", "ls"}, url, "")
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"svn", "ls", "--non-interactive", "--", "http://svn.example.org"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func httpBasicConfig() *fakeConfig {
	return newFakeConfig(map[string]any{
		"http-basic": php.ArrayOf("svn.apache.org", php.ArrayOf("username", "foo", "password", "bar")),
	})
}

func TestSvn_CredentialsFromConfig(t *testing.T) {
	svn := NewSvn("http://svn.apache.org", io.NewNullIO(), httpBasicConfig(), nil)

	if got, want := credentialArgs(t, svn), []string{"--username", "foo", "--password", "bar"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSvn_CredentialsFromConfigWithCacheCredentialsTrue(t *testing.T) {
	svn := NewSvn("http://svn.apache.org", io.NewNullIO(), httpBasicConfig(), nil)
	svn.SetCacheCredentials(true)

	if got, want := credentialArgs(t, svn), []string{"--username", "foo", "--password", "bar"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSvn_CredentialsFromConfigWithCacheCredentialsFalse(t *testing.T) {
	svn := NewSvn("http://svn.apache.org", io.NewNullIO(), httpBasicConfig(), nil)
	svn.SetCacheCredentials(false)

	if got, want := credentialArgs(t, svn), []string{"--no-auth-cache", "--username", "foo", "--password", "bar"}; !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The tests below cover Svn.php paths SvnTest leaves out.

func TestSvn_ExecuteRetriesWithPromptedCredentials(t *testing.T) {
	fio := newFakeIO()
	fio.interactive = true
	fio.ask = func(q string) any {
		if q == "Username: " {
			return "u"
		}

		return "p"
	}

	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("svn", "ls", "--non-interactive", "--", "http://svn.example.org/r", "dir"), Return: 1, Stderr: "svn: E170001: Authorization failed"},
		{Cmd: util.Cmd("svn", "ls", "--non-interactive", "--no-auth-cache", "--username", "u", "--password", "p", "--", "http://svn.example.org/r", "dir"), Stdout: "Redirecting to URL x\nok\n"},
	}, true, nil)

	svn := NewSvn("http://svn.example.org/r", fio, newFakeConfig(nil), process)

	out, err := svn.Execute([]string{"svn", "ls"}, "http://svn.example.org/r", "", "dir", false)
	if err != nil {
		t.Fatal(err)
	}

	// the mock hands all stdout over in one chunk, which starts with the
	// redirect notice and is therefore dropped as a whole, as in Composer
	if out != "" {
		t.Fatalf("output %q", out)
	}

	assertComplete(t, process)
}

func TestSvn_ExecuteNonAuthErrorAndNonInteractive(t *testing.T) {
	process := processmock.New()
	process.Expects([]processmock.Expectation{
		{Cmd: util.Cmd("svn", "info", "--non-interactive", "--", "http://x/r"), Return: 1, Stdout: "out", Stderr: "svn: E999: boom"},
		{Cmd: util.Cmd("svn", "info", "--non-interactive", "--", "http://x/r"), Return: 1, Stderr: "svn: E215004: auth"},
	}, true, nil)

	svn := NewSvn("http://x/r", io.NewNullIO(), newFakeConfig(nil), process)

	var re *util.RuntimeError

	_, err := svn.Execute([]string{"svn", "info"}, "http://x/r", "", "", false)
	if !errors.As(err, &re) || re.Message != "out\nsvn: E999: boom" {
		t.Fatalf("got %v", err)
	}

	_, err = svn.Execute([]string{"svn", "info"}, "http://x/r", "", "", false)
	if !errors.As(err, &re) || re.Message != "can not ask for authentication in non interactive mode" {
		t.Fatalf("got %v", err)
	}
}

func TestSvn_ExecuteGivesUpAfterMaxTries(t *testing.T) {
	process := processmock.New()
	process.Expects(nil, false, &processmock.Expectation{Return: 1, Stderr: "authorization failed"})

	svn := NewSvn("http://u:p@x/r", io.NewNullIO(), newFakeConfig(nil), process)

	_, err := svn.ExecuteLocal([]string{"svn", "up"}, "dir", "", false)
	if err == nil || err.Error() != "wrong credentials provided (authorization failed)" {
		t.Fatalf("got %v", err)
	}

	if n := len(process.Log()); n != svnMaxQtyAuthTries+1 {
		t.Fatalf("%d tries", n)
	}
}
