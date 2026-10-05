package util

import (
	"errors"
	"testing"

	"github.com/stubbedev/maestro/internal/php"
)

func TestForgejoUrl_Create(t *testing.T) {
	for _, repoURL := range []string{
		"git@codeberg.org:acme/repo.git",
		"https://codeberg.org/acme/repo",
		"https://codeberg.org/acme/repo.git",
	} {
		u, err := TryForgejoURL(repoURL)
		if err != nil || u == nil {
			t.Fatalf("%s: expected a Forgejo URL", repoURL)
		}

		if u.OriginURL != "codeberg.org" || u.Owner != "acme" || u.Repository != "repo" || u.APIURL != "https://codeberg.org/api/v1/repos/acme/repo" {
			t.Fatalf("%s: got %+v", repoURL, u)
		}
	}
}

func TestForgejoUrl_CreateInvalid(t *testing.T) {
	_, err := CreateForgejoURL("https://example.org")
	if ie, ok := errors.AsType[*InvalidArgumentError](err); !ok || ie.Message != "This is not a valid Forgejo URL: https://example.org" {
		t.Fatalf("got %v", err)
	}
}

func TestForgejoUrl_GenerateSshUrl(t *testing.T) {
	u, err := CreateForgejoURL("git@codeberg.org:acme/repo.git")
	if err != nil || u.GenerateSSHURL() != "git@codeberg.org:acme/repo.git" {
		t.Fatalf("got %v %v", u, err)
	}

	if u, _ := TryForgejoURL("HTTPS://CodeBerg.org/acme/repo/"); u != nil {
		t.Fatal("the scheme is matched case-sensitively")
	}

	if u, _ := TryForgejoURL("https://CodeBerg.org/acme/repo/"); u == nil || u.OriginURL != "codeberg.org" || u.Repository != "repo" {
		t.Fatalf("got %+v", u)
	}
}

func TestForgejoRepositoryData_FromRemoteData(t *testing.T) {
	data := php.ArrayOf(
		"html_url", "https://codeberg.org/acme/repo",
		"clone_url", "https://codeberg.org/acme/repo.git",
		"ssh_url", "git@codeberg.org:acme/repo.git",
		"private", false,
		"default_branch", "main",
		"has_issues", true,
		"archived", false,
	)

	d, err := ForgejoRepositoryDataFromRemote(data)
	if err != nil || d.DefaultBranch != "main" || !d.HasIssues || d.SSHURL != "git@codeberg.org:acme/repo.git" {
		t.Fatalf("got %+v %v", d, err)
	}

	data.Delete("private")

	if _, err := ForgejoRepositoryDataFromRemote(data); err == nil {
		t.Fatal("expected an error for a missing field")
	}
}

func TestTransportError_Types(t *testing.T) {
	var err error = NewMaxFileSizeExceededError("too big")

	te, ok := errors.AsType[*TransportError](err)
	if !ok || te.Message != "too big" || te.Code != 400 || err.Error() != "too big" {
		t.Fatalf("got %v", err)
	}

	if _, ok := errors.AsType[*MaxFileSizeExceededError](err); !ok {
		t.Fatal("expected a MaxFileSizeExceededException")
	}
}
