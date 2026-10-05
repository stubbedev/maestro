// Ports tests/Composer/Test/FilterList/FilterListApiClientTest.php
// (MIT, testdata/LICENSE-composer).

package filterlist_test

import (
	"testing"

	"github.com/stubbedev/maestro/internal/filterlist"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util/http/httpmock"
)

func filterBody() string {
	s, _ := php.JSONEncode(php.ArrayOf("filter", php.NewArray()), php.JSONPrettyPrint|php.JSONUnescapedSlashes|php.JSONUnescapedUnicode)

	return s
}

func assertEmptyFilter(t *testing.T, decoded any) {
	t.Helper()

	if !php.StrictEquals(decoded, php.ArrayOf("filter", php.NewArray())) {
		t.Errorf("decodeJson() = %#v", decoded)
	}
}

func TestFilterListApiClient_PostPurlsSendsPackagesAndListsAsBody(t *testing.T) {
	expectedAPIRequestBody, _ := php.JSONEncode(php.ArrayOf(
		"packages", php.ListOf("pkg://composer/vendor/foo", "pkg://composer/vendor/bar"),
		"lists", php.ListOf("malware", "typosquatting"),
	), 0)
	httpDownloader := httpmock.New()
	httpDownloader.Expects([]httpmock.Expectation{{
		URL: "https://example.org/api/filter",
		Options: php.ArrayOf("http", php.ArrayOf(
			"method", "POST",
			"header", php.ListOf("Content-type: application/json"),
			"timeout", 10,
			"content", expectedAPIRequestBody,
		)),
		Body: filterBody(),
	}}, true, nil)

	client := filterlist.NewFilterListApiClient(httpDownloader, nil)
	response, err := client.PostPurls("https://example.org/api/filter", constraintMap(
		"vendor/foo", eq("1.0.0.0"),
		"vendor/bar", eq("2.0.0.0"),
	), []string{"malware", "typosquatting"})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := response.DecodeJSON()
	if err != nil {
		t.Fatal(err)
	}
	assertEmptyFilter(t, decoded)
	if err := httpDownloader.AssertComplete(); err != nil {
		t.Error(err)
	}
}

func TestFilterListApiClient_PostPurlsSendsRepositoryTransportOptions(t *testing.T) {
	expectedAPIRequestBody, _ := php.JSONEncode(php.ArrayOf(
		"packages", php.ListOf("pkg://composer/vendor/foo"),
		"lists", php.ListOf("malware"),
	), 0)

	decode := func(s string) any {
		v, err := php.JSONDecode(s, false)
		if err != nil {
			t.Fatal(err)
		}

		return v
	}
	header := func(s string) any {
		v, _ := decode(s).(*php.Object).Get("header")

		return v
	}
	variationsThatShouldWork := []any{
		php.ObjectFromArray(php.ListOf("X-Cops: S07E12")),
		"X-Cops: S07E12",
		php.ListOf("X-Cops: S07E12"),
		header(`{"header": "X-Cops: S07E12"}`),
		header(`{"header": {"0": "X-Cops: S07E12"}}`),
		decode(`["X-Cops: S07E12"]`),
	}

	expectations := make([]httpmock.Expectation, len(variationsThatShouldWork))
	for i := range expectations {
		expectations[i] = httpmock.Expectation{
			URL: "https://example.org/api/filter",
			Options: php.ArrayOf(
				"ssl", php.ArrayOf("verify_peer", false),
				"http", php.ArrayOf(
					"header", php.ListOf("X-Cops: S07E12", "Content-type: application/json"),
					"method", "POST",
					"timeout", 10,
					"content", expectedAPIRequestBody,
				),
			),
			Body: filterBody(),
		}
	}
	httpDownloader := httpmock.New()
	httpDownloader.Expects(expectations, true, nil)

	for _, variation := range variationsThatShouldWork {
		client := filterlist.NewFilterListApiClient(httpDownloader, php.ArrayOf(
			"ssl", php.ArrayOf("verify_peer", false),
			"http", php.ArrayOf("header", variation),
		))
		response, err := client.PostPurls("https://example.org/api/filter", constraintMap("vendor/foo", eq("1.0.0.0")), []string{"malware"})
		if err != nil {
			t.Fatalf("%#v: %v", variation, err)
		}
		decoded, err := response.DecodeJSON()
		if err != nil {
			t.Fatal(err)
		}
		assertEmptyFilter(t, decoded)
	}
	if err := httpDownloader.AssertComplete(); err != nil {
		t.Error(err)
	}
}
