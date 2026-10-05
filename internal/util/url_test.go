package util

import "testing"

// Ports tests/Composer/Test/Util/UrlTest.php. Config is reduced to the
// github-domains and gitlab-domains values Url reads; their defaults are
// ["github.com"] and ["gitlab.com"], which config merging extends.

func TestUrl_UpdateDistReference(t *testing.T) {
	cases := []struct {
		url, expectedURL           string
		githubDomains, gitlabExtra []string
		ref                        string
	}{
		// github
		{"https://github.com/foo/bar/zipball/abcd", "https://api.github.com/repos/foo/bar/zipball/newref", nil, nil, ""},
		{"https://www.github.com/foo/bar/zipball/abcd", "https://api.github.com/repos/foo/bar/zipball/newref", nil, nil, ""},
		{"https://github.com/foo/bar/archive/abcd.zip", "https://api.github.com/repos/foo/bar/zipball/newref", nil, nil, ""},
		{"https://github.com/foo/bar/archive/abcd.tar.gz", "https://api.github.com/repos/foo/bar/tarball/newref", nil, nil, ""},
		{"https://api.github.com/repos/foo/bar/tarball", "https://api.github.com/repos/foo/bar/tarball/newref", nil, nil, ""},
		{"https://api.github.com/repos/foo/bar/tarball/abcd", "https://api.github.com/repos/foo/bar/tarball/newref", nil, nil, ""},

		// github enterprise
		{"https://mygithub.com/api/v3/repos/foo/bar/tarball/abcd", "https://mygithub.com/api/v3/repos/foo/bar/tarball/newref", []string{"mygithub.com"}, nil, ""},

		// bitbucket
		{"https://bitbucket.org/foo/bar/get/abcd.zip", "https://bitbucket.org/foo/bar/get/newref.zip", nil, nil, ""},
		{"https://www.bitbucket.org/foo/bar/get/abcd.tar.bz2", "https://bitbucket.org/foo/bar/get/newref.tar.bz2", nil, nil, ""},

		// gitlab
		{"https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=abcd", "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=newref", nil, nil, ""},
		{"https://www.gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=abcd", "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.zip?sha=newref", nil, nil, ""},
		{"https://gitlab.com/api/v3/projects/foo%2Fbar/repository/archive.tar.gz?sha=abcd", "https://gitlab.com/api/v4/projects/foo%2Fbar/repository/archive.tar.gz?sha=newref", nil, nil, ""},

		// gitlab enterprise
		{"https://mygitlab.com/api/v4/projects/foo%2Fbar/repository/archive.tar.gz?sha=abcd", "https://mygitlab.com/api/v4/projects/foo%2Fbar/repository/archive.tar.gz?sha=newref", nil, []string{"mygitlab.com"}, ""},
		{"https://mygitlab.com/api/v3/projects/foo%2Fbar/repository/archive.tar.bz2?sha=abcd", "https://mygitlab.com/api/v3/projects/foo%2Fbar/repository/archive.tar.bz2?sha=newref", nil, []string{"mygitlab.com"}, ""},
		{"https://mygitlab.com/api/v3/projects/foo%2Fbar/repository/archive.tar.bz2?sha=abcd", "https://mygitlab.com/api/v3/projects/foo%2Fbar/repository/archive.tar.bz2?sha=65", nil, []string{"mygitlab.com"}, "65"},
	}

	for _, c := range cases {
		ref := c.ref
		if ref == "" {
			ref = "newref"
		}

		githubDomains := append([]string{"github.com"}, c.githubDomains...)
		gitlabDomains := append([]string{"gitlab.com"}, c.gitlabExtra...)

		if got := UpdateDistReference(c.url, ref, githubDomains, gitlabDomains); got != c.expectedURL {
			t.Errorf("UpdateDistReference(%q, %q) = %q, want %q", c.url, ref, got, c.expectedURL)
		}
	}
}

func TestUrl_GetOrigin(t *testing.T) {
	cases := []struct {
		expected, url      string
		extraGitlabDomains []string
	}{
		// a host that is only a shorter prefix of a configured domain must not resolve to it
		{"gitlab.example.co", "https://gitlab.example.co/foo/bar/repository/archive.zip", []string{"gitlab.example.com"}},
		{"gitlab.example.co", "https://gitlab.example.co/foo/bar/repository/archive.zip", []string{"gitlab.example.co.uk/gitlab"}},
		{"gitlab.example.com", "https://gitlab.example.com/foo/bar/repository/archive.zip", []string{"gitlab.example.com"}},
		{"gitlab.example.com/gitlab", "https://gitlab.example.com/foo/bar/repository/archive.zip", []string{"gitlab.example.com/gitlab"}},
		// a configured domain may spell out a port the URL omits
		{"gitlab.example.com:443", "https://gitlab.example.com/foo/bar/repository/archive.zip", []string{"gitlab.example.com:443"}},
		{"gitlab.example.com:443/gitlab", "https://gitlab.example.com/foo/bar/repository/archive.zip", []string{"gitlab.example.com:443/gitlab"}},
	}

	for _, c := range cases {
		if got := GetOrigin(c.url, append([]string{"gitlab.com"}, c.extraGitlabDomains...)); got != c.expected {
			t.Errorf("GetOrigin(%q, %v) = %q, want %q", c.url, c.extraGitlabDomains, got, c.expected)
		}
	}
}

var urlSanitizeCases = []struct{ expected, url string }{
	// empty input safe (callers may pass `$x ?? ''` for nullable URLs)
	{"", ""},
	// with scheme
	{"https://foo:***@example.org/", "https://foo:bar@example.org/"},
	{"https://foo@example.org/", "https://foo@example.org/"},
	{"https://example.org/", "https://example.org/"},
	{"http://10a***:***@example.org", "http://10a8f08e8d7b7b9:foo@example.org"},
	{"https://foo:***@example.org:123/", "https://foo:bar@example.org:123/"},
	{"https://example.org/foo/bar?access_token=***", "https://example.org/foo/bar?access_token=abcdef"},
	{"https://example.org/foo/bar?foo=bar&access_token=***", "https://example.org/foo/bar?foo=bar&access_token=abcdef"},
	{"https://ghp***:***@github.com/acme/repo", "https://ghp_1234567890abcdefghijklmnopqrstuvwxyzAB:x-oauth-basic@github.com/acme/repo"},
	{"https://git***:***@github.com/acme/repo", "https://github_pat_1234567890abcdefghijkl_1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVW:x-oauth-basic@github.com/acme/repo"},
	{"http://abc***:***@example.org:123/", "http://abcdefghijkl:bar@example.org:123/"},
	{"https://abc***:***@example.org:123/", "https://abcdefghijklmnop:bar@example.org:123/"},
	// token/long username in the user slot without a password (e.g. https://TOKEN@host)
	{"https://ghp***@github.com/acme/repo", "https://ghp_1234567890abcdefghijklmnopqrstuvwxyzAB@github.com/acme/repo"},
	{"https://git***@github.com/acme/repo", "https://github_pat_1234567890abcdefghijkl_1234567890abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVW@github.com/acme/repo"},
	{"http://10a***@example.org", "http://10a8f08e8d7b7b9@example.org"},
	{"https://abc***@example.org:123/", "https://abcdefghijklmnop@example.org:123/"},
	// well-known non-secret credential markers are shown verbatim even though they are 12char+
	{"https://x-token-auth:***@bitbucket.org/acme/repo", "https://x-token-auth:secret@bitbucket.org/acme/repo"},
	{"https://gitlab-ci-token:***@gitlab.example.org/", "https://gitlab-ci-token:realtoken@gitlab.example.org/"},
	// without scheme
	{"foo:***@example.org/", "foo:bar@example.org/"},
	{"foo@example.org/", "foo@example.org/"},
	{"example.org/", "example.org/"},
	{"10a***:***@example.org", "10a8f08e8d7b7b9:foo@example.org"},
	{"foo:***@example.org:123/", "foo:bar@example.org:123/"},
	{"example.org/foo/bar?access_token=***", "example.org/foo/bar?access_token=abcdef"},
	{"example.org/foo/bar?foo=bar&access_token=***", "example.org/foo/bar?foo=bar&access_token=abcdef"},
	{"abc***:***@example.org:123/", "abcdefghijkl:bar@example.org:123/"},
	{"abc***:***@example.org:123/", "abcdefghijklmnop:bar@example.org:123/"},
	{"ghp***@github.com/acme/repo", "ghp_1234567890abcdefghijklmnopqrstuvwxyzAB@github.com/acme/repo"},
	{"10a***@example.org", "10a8f08e8d7b7b9@example.org"},
	{"abc***@example.org:123/", "abcdefghijklmnop@example.org:123/"},
	// anywhere in the string, as URLs usually reach sanitize() embedded in a longer message
	{"Failed to execute git clone --mirror -- https://foo:***@example.org/private/repo.git /cache/repo", "Failed to execute git clone --mirror -- https://foo:bar@example.org/private/repo.git /cache/repo"},
	{"tried https://foo:***@example.org/a and https://baz:***@example.com/b", "tried https://foo:bar@example.org/a and https://baz:qux@example.com/b"},
	{"fatal: unable to access 'https://gitlab-ci-token:***@example.org/g/r.git/'", "fatal: unable to access 'https://gitlab-ci-token:realtoken@example.org/g/r.git/'"},
	// passwords/usernames containing @ are masked up to the last @ of the authority
	{"https://foo:***@example.org/repo.git", "https://foo:bar@baz@example.org/repo.git"},
	{"https://use***:***@example.org/repo.git", "https://user@corp.example:realtoken@example.org/repo.git"},
	// empty user slot, e.g. https://:TOKEN@host
	{"https://:***@example.org/", "https://:realtoken@example.org/"},
	// schemes containing + . -
	{"git+ssh://foo:***@example.org/repo.git", "git+ssh://foo:bar@example.org/repo.git"},
	{"svn+ssh://foo:***@example.org/repo", "svn+ssh://foo:bar@example.org/repo"},
	// @ without credentials in front of it is left alone
	{"fatal: unable to access 'https://example.org/private/repo.git/': Could not resolve host", "fatal: unable to access 'https://example.org/private/repo.git/': Could not resolve host"},
	{"https://example.org/foo/bar@2x.png", "https://example.org/foo/bar@2x.png"},
}

func TestUrl_Sanitize(t *testing.T) {
	for _, c := range urlSanitizeCases {
		if got := SanitizeURL(c.url); got != c.expected {
			t.Errorf("SanitizeURL(%q) = %q, want %q", c.url, got, c.expected)
		}
	}
}

func TestUrl_SanitizeIsIdempotent(t *testing.T) {
	for _, c := range urlSanitizeCases {
		if got := SanitizeURL(SanitizeURL(c.url)); got != c.expected {
			t.Errorf("SanitizeURL(SanitizeURL(%q)) = %q, want %q", c.url, got, c.expected)
		}
	}
}

func TestUrl_IsAllowedRedirect(t *testing.T) {
	cases := []struct {
		expected bool
		url      string
	}{
		{true, "http://example.org/foo"},
		{true, "https://example.org/foo"},
		{true, "HTTPS://example.org/foo"},
		{false, "file://localhost/etc/passwd"},
		{false, "file:///etc/passwd"},
		{false, "phar://archive.phar/file"},
		{false, "data://text/plain;base64,Zm9v"},
		{false, "ftp://example.org/foo"},
		{false, "/foo/bar"},
		{false, "example.org/foo"},
	}

	for _, c := range cases {
		if got := IsAllowedRedirect(c.url); got != c.expected {
			t.Errorf("IsAllowedRedirect(%q) = %v, want %v", c.url, got, c.expected)
		}
	}
}

func TestUrl_StripCredentials(t *testing.T) {
	cases := []struct{ expected, url string }{
		{"https://example.org/repo.git", "https://user:pass@example.org/repo.git"},
		// a bare token in the user slot is a credential too, and has no colon to key off
		{"https://github.com/acme/repo.git", "https://ghp_1234567890abcdefghijklmnopqrstuvwxyzAB@github.com/acme/repo.git"},
		{"https://example.org/repo.git", "https://user:pa@ss@example.org/repo.git"},
		{"https://example.org:8080/repo.git", "https://user:pass@example.org:8080/repo.git"},
		{"https://example.org/repo.git?ref=a@b", "https://user:pass@example.org/repo.git?ref=a@b"},
		// nothing to strip
		{"https://example.org/repo.git", "https://example.org/repo.git"},
		{"https://example.org/@scope/repo.git", "https://example.org/@scope/repo.git"},
		{"git@example.org:acme/repo.git", "git@example.org:acme/repo.git"},
	}

	for _, c := range cases {
		if got := StripCredentials(c.url); got != c.expected {
			t.Errorf("StripCredentials(%q) = %q, want %q", c.url, got, c.expected)
		}
	}
}
