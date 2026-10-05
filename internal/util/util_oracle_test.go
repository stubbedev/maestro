package util

import (
	"encoding/json"
	"os"
	"sync"
	"testing"

	"github.com/stubbedev/maestro/internal/testutil"
)

// utilOracle is testdata/oracle/util.json.gz, written by
// tools/oracle/util/util.php from the PHP sources.
type utilOracle struct {
	Paths []struct {
		In              string `json:"in"`
		Normalized      string `json:"normalized"`
		Absolute        bool   `json:"absolute"`
		Trimmed         string `json:"trimmed"`
		Local           bool   `json:"local"`
		LocalWindows    bool   `json:"localWindows"`
		Platform        string `json:"platform"`
		PlatformWindows string `json:"platformWindows"`
	} `json:"paths"`
	Shortest []struct {
		From           string  `json:"from"`
		To             string  `json:"to"`
		Directories    bool    `json:"directories"`
		PreferRelative bool    `json:"preferRelative"`
		Path           *string `json:"path"`
		PathError      *string `json:"pathError"`
		Code           *string `json:"code"`
		CodeStatic     *string `json:"codeStatic"`
		CodeError      *string `json:"codeError"`
	} `json:"shortest"`
	URLs []struct {
		In       string          `json:"in"`
		Parsed   json.RawMessage `json:"parsed"`
		Sanitize string          `json:"sanitize"`
		Strip    string          `json:"strip"`
		Redirect bool            `json:"redirect"`
		Origin   *string         `json:"origin"`
		Ref      string          `json:"ref"`
		Dist     *string         `json:"dist"`
	} `json:"urls"`
	GitlabDomains []string `json:"gitlabDomains"`
	GithubDomains []string `json:"githubDomains"`
	Usernames     []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	} `json:"usernames"`
	NoProxy []struct {
		Pattern string   `json:"pattern"`
		URLs    []string `json:"urls"`
		Out     []bool   `json:"out"`
	} `json:"noproxy"`
	Filter []struct {
		In     string          `json:"in"`
		IP     bool            `json:"ip"`
		Port   json.RawMessage `json:"port"`
		Prefix json.RawMessage `json:"prefix"`
	} `json:"filter"`
	Expand []struct {
		In      string `json:"in"`
		Posix   string `json:"posix"`
		Windows string `json:"windows"`
	} `json:"expand"`
	Env    map[string]string `json:"env"`
	Mirror []struct {
		Kind      string  `json:"kind"`
		Mirror    string  `json:"mirror"`
		Name      string  `json:"name"`
		Version   string  `json:"version"`
		URL       string  `json:"url"`
		Reference *string `json:"reference"`
		Type      *string `json:"type"`
		Pretty    *string `json:"pretty"`
		Out       string  `json:"out"`
	} `json:"mirror"`
}

var loadUtilOracle = sync.OnceValues(func() (*utilOracle, error) {
	data, err := testutil.ReadGoldenFile("testdata/oracle/util.json.gz")
	if err != nil {
		return nil, err
	}

	var o utilOracle

	return &o, json.Unmarshal(data, &o)
})

func utilOracleData(t *testing.T) *utilOracle {
	t.Helper()

	o, err := loadUtilOracle()
	if err != nil {
		t.Fatal(err)
	}

	return o
}

func TestOracle_FilesystemPaths(t *testing.T) {
	o := utilOracleData(t)

	for _, c := range o.Paths {
		if got := NormalizePath(c.In); got != c.Normalized {
			t.Errorf("NormalizePath(%q) = %q, want %q", c.In, got, c.Normalized)
		}

		if got := IsAbsolutePath(c.In); got != c.Absolute {
			t.Errorf("IsAbsolutePath(%q) = %v, want %v", c.In, got, c.Absolute)
		}

		if got := TrimTrailingSlash(c.In); got != c.Trimmed {
			t.Errorf("TrimTrailingSlash(%q) = %q, want %q", c.In, got, c.Trimmed)
		}

		if got := isLocalPath(c.In, false); got != c.Local {
			t.Errorf("isLocalPath(%q, posix) = %v, want %v", c.In, got, c.Local)
		}

		if got := isLocalPath(c.In, true); got != c.LocalWindows {
			t.Errorf("isLocalPath(%q, windows) = %v, want %v", c.In, got, c.LocalWindows)
		}

		if got := getPlatformPath(c.In, false); got != c.Platform {
			t.Errorf("getPlatformPath(%q, posix) = %q, want %q", c.In, got, c.Platform)
		}

		if got := getPlatformPath(c.In, true); got != c.PlatformWindows {
			t.Errorf("getPlatformPath(%q, windows) = %q, want %q", c.In, got, c.PlatformWindows)
		}
	}
}

func TestOracle_FilesystemFindShortestPath(t *testing.T) {
	o := utilOracleData(t)

	check := func(what string, c any, got string, err error, want, wantErr *string) {
		t.Helper()

		switch {
		case wantErr != nil:
			if err == nil || err.Error() != *wantErr {
				t.Errorf("%s %+v: got %q, %v; want error %q", what, c, got, err, *wantErr)
			}
		case err != nil:
			t.Errorf("%s %+v: unexpected error %v", what, c, err)
		case got != *want:
			t.Errorf("%s %+v: got %q, want %q", what, c, got, *want)
		}
	}

	for _, c := range o.Shortest {
		if c.Path != nil || c.PathError != nil {
			got, err := findShortestPath(c.From, c.To, c.Directories, c.PreferRelative, false)
			check("findShortestPath", c, got, err, c.Path, c.PathError)
		}

		if c.Code != nil || c.CodeError != nil {
			got, err := findShortestPathCode(c.From, c.To, c.Directories, false, c.PreferRelative, false)
			check("findShortestPathCode", c, got, err, c.Code, c.CodeError)

			got, err = findShortestPathCode(c.From, c.To, c.Directories, true, c.PreferRelative, false)
			check("findShortestPathCode(static)", c, got, err, c.CodeStatic, c.CodeError)
		}
	}
}

// phpParsedURL renders parseURL's result as PHP's parse_url() array.
func phpParsedURL(u phpURL, ok bool) any {
	if !ok {
		return false
	}

	m := map[string]any{}

	add := func(has bool, key string, value any) {
		if has {
			m[key] = value
		}
	}

	add(u.hasScheme, "scheme", u.scheme)
	add(u.hasHost, "host", u.host)
	add(u.hasPort, "port", float64(u.port))
	add(u.hasUser, "user", u.user)
	add(u.hasPass, "pass", u.pass)
	add(u.hasPath, "path", u.path)
	add(u.hasQuery, "query", u.query)
	add(u.hasFragment, "fragment", u.fragment)

	return m
}

func TestOracle_URL(t *testing.T) {
	o := utilOracleData(t)

	for _, c := range o.URLs {
		var want any
		if err := json.Unmarshal(c.Parsed, &want); err != nil {
			t.Fatal(err)
		}

		gotParsed, _ := json.Marshal(phpParsedURL(parseURL(c.In)))
		wantParsed, _ := json.Marshal(want)

		if string(gotParsed) != string(wantParsed) {
			t.Errorf("parse_url(%q) = %s, want %s", c.In, gotParsed, wantParsed)
		}

		if got := SanitizeURL(c.In); got != c.Sanitize {
			t.Errorf("SanitizeURL(%q) = %q, want %q", c.In, got, c.Sanitize)
		}

		if got := StripCredentials(c.In); got != c.Strip {
			t.Errorf("StripCredentials(%q) = %q, want %q", c.In, got, c.Strip)
		}

		if got := IsAllowedRedirect(c.In); got != c.Redirect {
			t.Errorf("IsAllowedRedirect(%q) = %v, want %v", c.In, got, c.Redirect)
		}

		if c.Origin != nil {
			if got := GetOrigin(c.In, o.GitlabDomains); got != *c.Origin {
				t.Errorf("GetOrigin(%q) = %q, want %q", c.In, got, *c.Origin)
			}
		}

		if c.Dist != nil {
			if got, err := UpdateDistReference(c.In, c.Ref, o.GithubDomains, o.GitlabDomains); err != nil || got != *c.Dist {
				t.Errorf("UpdateDistReference(%q, %q) = %q, %v, want %q", c.In, c.Ref, got, err, *c.Dist)
			}
		}
	}

	for _, c := range o.Usernames {
		if got := SanitizeUsername(c.In); got != c.Out {
			t.Errorf("SanitizeUsername(%q) = %q, want %q", c.In, got, c.Out)
		}
	}
}

func TestOracle_NoProxyPattern(t *testing.T) {
	o := utilOracleData(t)

	for _, c := range o.NoProxy {
		matcher := NewNoProxyPattern(c.Pattern)

		for i, url := range c.URLs {
			if got := matcher.Test(url); got != c.Out[i] {
				t.Errorf("NoProxyPattern(%q).Test(%q) = %v, want %v", c.Pattern, url, got, c.Out[i])
			}
		}
	}

	for _, c := range o.Filter {
		if got := filterValidateIP(c.In); got != c.IP {
			t.Errorf("filterValidateIP(%q) = %v, want %v", c.In, got, c.IP)
		}

		for _, r := range []struct {
			want     json.RawMessage
			min, max int
		}{{c.Port, 1, 65535}, {c.Prefix, 0, 128}} {
			value, ok := filterValidateInt(c.In, r.min, r.max)

			got := "false"
			if ok {
				got, _ = func() (string, error) { b, err := json.Marshal(value); return string(b), err }()
			}

			if got != string(r.want) {
				t.Errorf("filterValidateInt(%q, %d, %d) = %s, want %s", c.In, r.min, r.max, got, r.want)
			}
		}
	}
}

func TestOracle_PlatformExpandPath(t *testing.T) {
	o := utilOracleData(t)

	for k, v := range o.Env {
		t.Setenv(k, v)
	}

	t.Setenv("UNSET_VAR", "")
	os.Unsetenv("UNSET_VAR")

	for _, c := range o.Expand {
		if got, err := expandPath(c.In, false); err != nil || got != c.Posix {
			t.Errorf("expandPath(%q, posix) = %q, %v; want %q", c.In, got, err, c.Posix)
		}

		if got, err := expandPath(c.In, true); err != nil || got != c.Windows {
			t.Errorf("expandPath(%q, windows) = %q, %v; want %q", c.In, got, err, c.Windows)
		}
	}
}

func TestOracle_ComposerMirror(t *testing.T) {
	o := utilOracleData(t)

	for _, c := range o.Mirror {
		var got string

		if c.Kind == "url" {
			got = ComposerMirrorProcessURL(c.Mirror, c.Name, c.Version, c.Reference, c.Type, c.Pretty)
		} else {
			got = ComposerMirrorProcessGitURL(c.Mirror, c.Name, c.URL, c.Type)
		}

		if got != c.Out {
			t.Errorf("ComposerMirror %+v: got %q, want %q", c, got, c.Out)
		}
	}
}
