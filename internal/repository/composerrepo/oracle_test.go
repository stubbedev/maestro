package composerrepo

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	stdio "io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/console"
	"github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/pkg/dumper"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/semver"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

const oracleFlags = php.JSONUnescapedSlashes | php.JSONUnescapedUnicode | php.JSONPreserveZeroFraction

const p2Fixtures = "../../pkg/loader/testdata/oracle/p2"

// readGzip reads a gzipped file.
func readGzip(t testing.TB, path string) string {
	t.Helper()

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	data, err := stdio.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

// fakeServer is the oracle's FakeServer: a repository server answering
// from a file map, with Last-Modified / If-Modified-Since handling.
type fakeServer struct {
	t     *testing.T
	files *php.Array
	log   *php.Array
}

func (s *fakeServer) Get(url string, options *php.Array) (*http.Response, error) {
	s.log.Append(php.ListOf(url, cloneOptions(options)))

	return s.serve(url, options)
}

func (s *fakeServer) Add(url string, options *php.Array) (*util.Promise[*http.Response], error) {
	s.log.Append(php.ListOf(url, cloneOptions(options)))
	r, err := s.serve(url, options)
	if err != nil {
		return util.Rejected[*http.Response](err), nil
	}

	return util.Resolved(r), nil
}

func (s *fakeServer) serve(url string, options *php.Array) (*http.Response, error) {
	file, _ := get(s.files, url).(*php.Array)
	if file == nil {
		file = php.ArrayOf("status", 404)
	}
	status := 200
	if v := get(file, "status"); v != nil {
		status = int(php.ToInt(v))
	}
	if status >= 400 {
		e := util.NewTransportError(`The "`+url+`" file could not be downloaded (HTTP/1.1 `+itoa(status)+` Error)`, status)
		e.StatusCode = status

		return nil, e
	}

	headers := []string{"HTTP/1.1 200 OK"}
	if lastModified, ok := file.GetString("lastModified"); ok {
		httpOptions, _ := options.GetArray("http")
		if slices.Contains(asHeaderList(get(httpOptions, "header")), "If-Modified-Since: "+lastModified) {
			return http.NewResponse(url, 304, []string{"HTTP/1.1 304 Not Modified"}, ""), nil
		}
		headers = append(headers, "Last-Modified: "+lastModified)
	}

	body, ok := file.GetString("body")
	if fixture, isFixture := file.GetString("fixture"); isFixture {
		body, ok = readGzip(s.t, p2Fixtures+"/"+fixture+".json.gz"), true
	}
	if !ok {
		s.t.Fatalf("no body for %s", url)
	}

	return http.NewResponse(url, 200, headers, body), nil
}

// asHeaderList is (array) $header.
func asHeaderList(v any) []string {
	switch v := v.(type) {
	case nil:
		return nil
	case *php.Array:
		var out []string
		for _, h := range v.All() {
			out = append(out, php.ToString(h))
		}

		return out
	}

	return []string{php.ToString(v)}
}

func itoa(i int) string { return php.ToString(int64(i)) }

func shortClass(class string) string { return class[strings.LastIndexByte(class, '\\')+1:] }

// describe is the oracle's describe().
func describe(t *testing.T, p pkg.PackageInterface) any {
	t.Helper()

	if p == nil {
		return nil
	}
	var aliasOf, repoName any
	if alias, ok := p.(pkg.Alias); ok {
		aliasOf = alias.AliasOf().UniqueName()
	}
	if repo := p.Repository(); repo != nil {
		repoName = repo.RepoName()
	}
	dump, err := dumper.ArrayDumper{}.Dump(p)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := php.JSONEncode(dump, oracleFlags)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(encoded))

	return php.ListOf(
		shortClass(p.Class()),
		p.UniqueName(),
		p.PrettyVersion(),
		aliasOf,
		repoName,
		p.NotificationURL().Value(),
		php.StringList(p.DistURLs()),
		php.StringList(p.SourceURLs()),
		p.TransportOptions(),
		hex.EncodeToString(sum[:]),
	)
}

func describeAll(t *testing.T, packages []pkg.PackageInterface) *php.Array {
	out := php.NewArray()
	for _, p := range packages {
		out.Append(describe(t, p))
	}

	return out
}

// constraintMap is the oracle's constraint_map().
func constraintMap(t *testing.T, m *php.Array) *repository.ConstraintMap {
	t.Helper()

	out := &repository.ConstraintMap{}
	for k, v := range m.All() {
		switch v {
		case nil:
			out.Set(k.String(), nil)
		case "*":
			out.Set(k.String(), semver.NewMatchAllConstraint())
		default:
			out.Set(k.String(), must(repository.ParseConstraint(php.ToString(v))))
		}
	}

	return out
}

func strList(list []string) *php.Array { return php.StringList(list) }

// errorClass names the PHP class of an error the repository returns.
func errorClass(err error) string {
	var securityErr *repository.SecurityError
	var transport *util.TransportError
	switch {
	case errors.As(err, &securityErr):
		return `Composer\Repository\RepositorySecurityException`
	case errors.As(err, &transport):
		return `Composer\Downloader\TransportException`
	}

	return exceptionClass(err)
}

// runStep is the oracle's run_step().
func runStep(t *testing.T, r *ComposerRepository, step *php.Array) (any, error) {
	t.Helper()

	str := func(key string) string { return php.ToString(get(step, key)) }
	arr := func(key string) *php.Array { return asArray(get(step, key)) }
	constraint := func() semver.ConstraintInterface {
		if v := get(step, "constraint"); v != nil {
			return must(repository.ParseConstraint(php.ToString(v)))
		}

		return nil
	}

	switch str("op") {
	case "loadPackages":
		alreadyLoaded := repository.AlreadyLoaded{}
		for name, versions := range arr("alreadyLoaded").All() {
			alreadyLoaded[name.String()] = map[string]pkg.PackageInterface{}
			for _, v := range asArray(versions).All() {
				alreadyLoaded[name.String()][php.ToString(v)] = pkg.NewPackage(name.String(), php.ToString(v), php.ToString(v))
			}
		}
		result, err := r.LoadPackages(constraintMap(t, arr("names")), arr("acceptable"), arr("flags"), alreadyLoaded)
		if err != nil {
			return nil, err
		}

		return php.ArrayOf("namesFound", strList(result.NamesFound), "packages", describeAll(t, result.Packages)), nil
	case "findPackage":
		p, err := r.FindPackage(str("name"), constraint())

		return describe(t, p), err
	case "findPackages":
		packages, err := r.FindPackages(str("name"), constraint())

		return describeAll(t, packages), err
	case "getPackages":
		packages, err := r.Packages()

		return describeAll(t, packages), err
	case "count":
		n, err := r.Count()

		return int64(n), err
	case "getPackageNames":
		names, err := r.PackageNames(str("filter"))

		return strList(names), err
	case "search":
		results, err := r.Search(str("query"), int(php.ToInt(get(step, "mode"))), str("type"))
		out := php.NewArray()
		for _, result := range results {
			if result.Raw != nil {
				out.Append(result.Raw)

				continue
			}
			entry := php.ArrayOf("name", result.Name, "description", result.Description.Value())
			if result.Abandoned != nil {
				entry.Set("abandoned", result.Abandoned)
			}
			if result.URL.Valid {
				entry.Set("url", result.URL.S)
			}
			out.Append(entry)
		}

		return out, err
	case "getProviders":
		providers, err := r.Providers(str("name"))
		out := php.NewArray()
		for _, p := range providers {
			out.Append(php.ListOf(p.Name, p.Description.Value(), p.Type))
		}

		return out, err
	case "hasSecurityAdvisories":
		return r.HasSecurityAdvisories()
	case "getSecurityAdvisories":
		result, err := r.SecurityAdvisories(constraintMap(t, arr("map")), php.ToBool(get(step, "partial")))
		if err != nil {
			return nil, err
		}
		advisories := php.NewArray()
		for name, list := range result.Advisories.All() {
			l := php.NewArray()
			for _, a := range list {
				class := "PartialSecurityAdvisory"
				if _, ok := a.(*repository.SecurityAdvisory); ok {
					class = "SecurityAdvisory"
				}
				l.Append(php.ListOf(class, a.JSONSerialize()))
			}
			advisories.Set(name, l)
		}

		return php.ArrayOf("namesFound", strList(result.NamesFound), "advisories", advisories), nil
	case "hasFilter":
		return r.HasFilter()
	case "getFilterLists":
		lists, err := r.FilterLists()

		return strList(lists), err
	case "getFilter":
		lists := make([]string, 0)
		for _, v := range arr("lists").All() {
			lists = append(lists, php.ToString(v))
		}
		filter, err := r.Filter(constraintMap(t, arr("map")), lists)
		if err != nil {
			return nil, err
		}
		out := php.NewArray()
		for list, entries := range filter.All() {
			l := php.NewArray()
			for _, e := range entries {
				l.Append(php.ListOf(e.PackageName, e.ListName, e.Constraint.PrettyString(), e.Constraint.String(), e.URL.Value(), e.Reason.Value(), e.ID.Value(), e.Source.Value()))
			}
			out.Set(list, l)
		}

		return out, nil
	}

	t.Fatalf("unknown op %s", str("op"))

	return nil, nil
}

// cacheFiles is the oracle's cache_files().
func cacheFiles(t *testing.T, dir string) *php.Array {
	t.Helper()

	var paths []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			paths = append(paths, path)
		}

		return nil
	})
	slices.Sort(paths)
	out := php.NewArray()
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		out.Set(path[len(dir)+1:], hex.EncodeToString(sum[:]))
	}

	return out
}

func encodeOracle(t *testing.T, v any) string {
	t.Helper()

	s, err := php.JSONEncode(v, oracleFlags)
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func newBufferIO(t *testing.T) *io.BufferIO {
	t.Helper()

	b, err := io.NewBufferIO("", console.VerbosityVerbose, nil)
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// TestComposerRepository_Oracle replays the scenarios of
// tools/oracle/composerrepo/oracle.php: the requests, results, output and
// metadata cache files must be Composer's.
func TestComposerRepository_Oracle(t *testing.T) {
	golden := decodeArray(readGzip(t, "testdata/oracle/composerrepo.json.gz"))

	for name, raw := range golden.All() {
		t.Run(name.String(), func(t *testing.T) {
			scenario := raw.(*php.Array)
			tmp := t.TempDir()
			server := &fakeServer{t: t, files: asArray(get(scenario, "files")).Clone()}
			cfg := createConfig(t, "home", tmp, "cache-dir", tmp+"/cache")
			cacheDir := tmp + "/cache/repo"

			results := asArray(get(scenario, "results")).Values()
			var (
				repo *ComposerRepository
				out  *io.BufferIO
			)
			for i, rawStep := range asArray(get(scenario, "steps")).Values() {
				step := rawStep.(*php.Array)
				for url, file := range asArray(get(step, "files")).All() {
					server.files.Set(url.String(), file)
				}
				server.log = php.NewArray()

				var (
					result any
					err    error
				)
				if get(step, "op") == "new" {
					out = newBufferIO(t)
					var r *ComposerRepository
					r, err = New(asArray(get(step, "config")), out, cfg, server, nil)
					if err == nil {
						repo = r
						result = r.RepoName()
					}
				} else {
					result, err = runStep(t, repo, step)
				}
				if err != nil {
					result = php.ArrayOf("e", php.ListOf(errorClass(err), err.Error()))
				}

				want := results[i].(*php.Array)
				label := php.ToString(get(step, "op"))
				if got, w := encodeOracle(t, server.log), encodeOracle(t, get(want, "requests")); got != w {
					t.Errorf("step %d %s: requests\n got %s\nwant %s", i, label, got, w)
				}
				if got, w := encodeOracle(t, result), encodeOracle(t, get(want, "result")); got != w {
					t.Errorf("step %d %s: result\n got %s\nwant %s", i, label, got, w)
				}
				if got, w := out.Output(), php.ToString(get(want, "output")); got != w {
					t.Errorf("step %d %s: output\n got %q\nwant %q", i, label, got, w)
				}
				if got, w := encodeOracle(t, cacheFiles(t, cacheDir)), encodeOracle(t, get(want, "cache")); got != w {
					t.Errorf("step %d %s: cache\n got %s\nwant %s", i, label, got, w)
				}

				out = newBufferIO(t)
				if repo != nil {
					repo.io = out
				}
			}
		})
	}
}
