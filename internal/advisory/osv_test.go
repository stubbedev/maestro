package advisory_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/maestro/internal/advisory"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
	"github.com/stubbedev/maestro/internal/util/http"
)

type osvRequest struct {
	url     string
	body    map[string]any
	timeout any
}

type fakeOSV struct {
	t        *testing.T
	listed   map[string][]map[string]string
	records  map[string][][]map[string]any
	fail     error
	requests []osvRequest
}

func (f *fakeOSV) Get(url string, options *php.Array) (*http.Response, error) {
	f.t.Helper()
	httpOptions, _ := options.GetArray("http")
	content, _ := httpOptions.GetString("content")
	timeout, _ := httpOptions.Get("timeout")
	var body map[string]any
	if err := json.Unmarshal([]byte(content), &body); err != nil {
		f.t.Fatalf("request to %s: %v", url, err)
	}
	f.requests = append(f.requests, osvRequest{url, body, timeout})
	if f.fail != nil {
		return nil, f.fail
	}

	var answer any
	switch url {
	case "https://api.osv.dev/v1/querybatch":
		results := []any{}
		for _, q := range body["queries"].([]any) {
			name := q.(map[string]any)["package"].(map[string]any)["name"].(string)
			results = append(results, map[string]any{"vulns": f.listed[name]})
		}
		answer = map[string]any{"results": results}
	case "https://api.osv.dev/v1/query":
		pkgQuery := body["package"].(map[string]any)
		if pkgQuery["ecosystem"] != "Packagist" {
			f.t.Fatalf("query for ecosystem %v", pkgQuery["ecosystem"])
		}
		pages := f.records[pkgQuery["name"].(string)]
		page := 0
		if token, ok := body["page_token"].(string); ok {
			page = int(token[0] - '0')
		}
		result := map[string]any{"vulns": pages[page]}
		if page+1 < len(pages) {
			result["next_page_token"] = string(rune('0' + page + 1))
		}
		answer = result
	default:
		f.t.Fatalf("unexpected request to %s", url)
	}
	encoded, err := json.Marshal(answer)
	if err != nil {
		f.t.Fatal(err)
	}

	return http.NewResponse(url, 200, nil, string(encoded)), nil
}

func (f *fakeOSV) urls() []string {
	urls := make([]string, len(f.requests))
	for i, r := range f.requests {
		urls[i] = r.url
	}
	f.requests = nil

	return urls
}

func osvVuln(id, modified string, aliases []string, events ...map[string]string) map[string]any {
	return map[string]any{
		"id":        id,
		"summary":   "summary of " + id,
		"aliases":   aliases,
		"modified":  modified,
		"published": "2024-03-01T10:00:00Z",
		"affected": []any{map[string]any{
			"package": map[string]any{"name": "vendor1/package1", "ecosystem": "Packagist"},
			"ranges":  []any{map[string]any{"type": "ECOSYSTEM", "events": events}},
		}},
	}
}

func newFakeOSV(t *testing.T) *fakeOSV {
	drupal := map[string]any{
		"id":       "DRUPAL-CORE-2019-009",
		"details":  "A visit to install.php can corrupt cached data.\nMore details.",
		"modified": "2026-09-19T19:06:41Z",
		"affected": []any{map[string]any{
			"package": map[string]any{"name": "vendor9/osvonly", "ecosystem": "Packagist"},
			"ranges":  []any{map[string]any{"type": "ECOSYSTEM", "events": []map[string]string{{"introduced": "0"}, {"fixed": "2.0"}}}},
		}},
		"database_specific": map[string]any{"severity": "MODERATE"},
	}

	return &fakeOSV{
		t: t,
		listed: map[string][]map[string]string{
			"vendor1/package1": {
				{"id": "GHSA-dupe", "modified": "m1"},
				{"id": "OSV-2", "modified": "m1"},
				{"id": "OSV-3", "modified": "m1"},
			},
			"vendor9/osvonly": {{"id": "DRUPAL-CORE-2019-009", "modified": "2026-09-19T19:06:41Z"}},
		},
		records: map[string][][]map[string]any{
			"vendor1/package1": {
				{
					osvVuln("GHSA-dupe", "m1", []string{"CVE1"}, map[string]string{"introduced": "0"}),
					osvVuln("OSV-2", "m1", nil, map[string]string{"introduced": "0"}, map[string]string{"fixed": "3.1"}),
				},
				{osvVuln("OSV-3", "m1", nil, map[string]string{"introduced": "4.0"})},
			},
			"vendor9/osvonly": {{drupal}},
		},
	}
}

func osvPackages() []pkg.PackageInterface {
	return []pkg.PackageInterface{
		pkg.NewPackage("vendor1/package1", "3.0.0.0", "3.0.0"),
		pkg.NewPackage("vendor9/osvonly", "1.0.0.0", "1.0.0"),
	}
}

func auditJSON(t *testing.T, auditor advisory.Auditor) (int, *php.Array) {
	t.Helper()
	b := newBufferIO(t)
	status, err := auditor.Audit(b, getRepoSet(t), createPolicyConfig(policyOptions{}), osvPackages(), advisory.FormatJSON, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := php.JSONDecode(b.Output(), true)
	if err != nil {
		t.Fatal(err)
	}
	advisories, _ := decoded.(*php.Array).GetArray("advisories")

	return status, advisories
}

func packageNames(advisories *php.Array) []string {
	var names []string
	for _, k := range advisories.Keys() {
		names = append(names, k.String())
	}

	return names
}

func advisoryIDs(t *testing.T, advisories *php.Array, name string) []string {
	t.Helper()
	list, _ := advisories.GetArray(name)
	var ids []string
	for _, a := range list.Values() {
		id, _ := a.(*php.Array).GetString("advisoryId")
		ids = append(ids, id)
	}

	return ids
}

func TestAuditor_AuditAddsOSVAdvisories(t *testing.T) {
	status, advisories := auditJSON(t, advisory.Auditor{})
	if status != advisory.StatusFailed || !slices.Equal(packageNames(advisories), []string{"vendor1/package1"}) {
		t.Fatalf("without OSV: status %d, advisories of %q", status, packageNames(advisories))
	}

	osv := newFakeOSV(t)
	_, advisories = auditJSON(t, advisory.Auditor{OSV: advisory.NewOSV(osv, t.TempDir())})
	if got := packageNames(advisories); !slices.Equal(got, []string{"vendor1/package1", "vendor9/osvonly"}) {
		t.Fatalf("advisories of %q", got)
	}
	if got := advisoryIDs(t, advisories, "vendor1/package1"); !slices.Equal(got, []string{"ID1", "OSV-2"}) {
		t.Errorf("vendor1/package1's advisories %q: OSV's duplicate of ID1 (by its CVE alias) and the one not affecting 3.0.0 must be left out", got)
	}

	list, _ := advisories.GetArray("vendor9/osvonly")
	drupal := list.Values()[0].(*php.Array)
	want := map[string]any{
		"advisoryId":       "DRUPAL-CORE-2019-009",
		"packageName":      "vendor9/osvonly",
		"affectedVersions": "<2.0",
		"title":            "A visit to install.php can corrupt cached data.",
		"cve":              nil,
		"link":             "https://osv.dev/vulnerability/DRUPAL-CORE-2019-009",
		"reportedAt":       "2026-09-19T19:06:41+00:00",
		"severity":         "medium",
	}
	for key, value := range want {
		if got, _ := drupal.Get(key); got != value {
			t.Errorf("%s = %#v, want %#v", key, got, value)
		}
	}
	sources, _ := drupal.GetArray("sources")
	if encoded, _ := php.JSONEncode(sources, 0); encoded != `[{"name":"OSV","remoteId":"DRUPAL-CORE-2019-009"}]` {
		t.Errorf("sources = %s", encoded)
	}

	if got := osv.requests[0].body["queries"]; len(got.([]any)) != 2 {
		t.Errorf("querybatch queries %v", got)
	}
	for _, r := range osv.requests {
		if r.timeout != int64(10) {
			t.Errorf("request to %s has timeout %v", r.url, r.timeout)
		}
	}
	if page := osv.requests[2].body["page_token"]; page != "1" {
		t.Errorf("second page requested with token %v", page)
	}
	if got := osv.urls(); !slices.Equal(got, []string{
		"https://api.osv.dev/v1/querybatch",
		"https://api.osv.dev/v1/query", "https://api.osv.dev/v1/query",
		"https://api.osv.dev/v1/query",
	}) {
		t.Errorf("requests %q", got)
	}
}

func TestOSV_KeepsRecordsWhileOSVListsThemUnchanged(t *testing.T) {
	osv := newFakeOSV(t)
	auditor := advisory.Auditor{OSV: advisory.NewOSV(osv, t.TempDir())}
	auditJSON(t, auditor)
	osv.urls()

	_, advisories := auditJSON(t, auditor)
	if got := osv.urls(); !slices.Equal(got, []string{"https://api.osv.dev/v1/querybatch"}) {
		t.Errorf("requests with every record kept: %q", got)
	}
	if got := advisoryIDs(t, advisories, "vendor1/package1"); !slices.Equal(got, []string{"ID1", "OSV-2"}) {
		t.Errorf("advisories from the kept records: %q", got)
	}

	osv.listed["vendor1/package1"][1]["modified"] = "m2"
	auditJSON(t, auditor)
	if got := osv.urls(); !slices.Equal(got, []string{
		"https://api.osv.dev/v1/querybatch", "https://api.osv.dev/v1/query", "https://api.osv.dev/v1/query",
	}) {
		t.Errorf("requests after a record changed: %q", got)
	}
}

func TestOSV_UnreachableFollowsThePolicy(t *testing.T) {
	osv := newFakeOSV(t)
	osv.fail = util.NewTransportError(`The "https://api.osv.dev/v1/querybatch" file could not be downloaded (HTTP/2 503 )`, 503)
	auditor := advisory.Auditor{OSV: advisory.NewOSV(osv, "")}

	_, err := auditor.Audit(newBufferIO(t), getRepoSet(t), createPolicyConfig(policyOptions{}), osvPackages(), advisory.FormatJSON, false, nil)
	if transport, ok := errors.AsType[*util.TransportError](err); !ok || transport.Code != 503 {
		t.Fatalf("error %v, want OSV's transport error", err)
	}

	b := newBufferIO(t)
	status, err := auditor.Audit(b, getRepoSet(t), createPolicyConfig(policyOptions{ignoreUnreachableAudit: true}), osvPackages(), advisory.FormatJSON, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if status != advisory.StatusFailed || !strings.Contains(b.Output(), `"unreachable-repositories": [`) || !strings.Contains(b.Output(), "api.osv.dev") || !strings.Contains(b.Output(), `"ID1"`) {
		t.Errorf("status %d, output:\n%s", status, b.Output())
	}
}
