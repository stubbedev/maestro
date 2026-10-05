// Ports src/Composer/FilterList/FilterListApiClient.php.

package filterlist

import (
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/repository"
	"github.com/stubbedev/maestro/internal/util/http"
)

// FilterListApiClient ports Composer\FilterList\FilterListApiClient.
type FilterListApiClient struct {
	httpDownloader http.Getter
	options        *php.Array
}

// NewFilterListApiClient ports new FilterListApiClient($httpDownloader,
// $options); options (stream context options) may be nil.
func NewFilterListApiClient(httpDownloader http.Getter, options *php.Array) *FilterListApiClient {
	return &FilterListApiClient{httpDownloader: httpDownloader, options: options}
}

// PostPurls ports postPurls: it POSTs the PURLs of the packages (and the
// configured list names) to a remote filter list endpoint.
func (c *FilterListApiClient) PostPurls(url string, packageConstraintMap *repository.ConstraintMap, configuredLists []string) (*http.Response, error) {
	purls := php.NewArrayCap(packageConstraintMap.Len())
	for packageName := range packageConstraintMap.All() {
		purls.Append("pkg://composer/" + packageName)
	}

	body := php.ArrayOf("packages", purls, "lists", php.StringList(configuredLists))
	content, err := php.JSONEncode(body, 0)
	if err != nil {
		return nil, err
	}

	return c.httpDownloader.Get(url, PostOptions(c.options, "Content-type: application/json", content))
}

// PostOptions returns a copy of the stream context options for a POST
// request with the given Content-type header and body, as
// FilterListApiClient and ComposerRepository build them:
// method POST, the header appended to http.header (made a list), a 10
// second timeout, and the content.
func PostOptions(options *php.Array, contentTypeHeader, content string) *php.Array {
	if options == nil {
		options = php.NewArray()
	} else {
		options = options.Clone()
	}

	httpOptions := HTTPOptions(options)
	httpOptions.Set("method", "POST")
	AppendHeader(httpOptions, contentTypeHeader)
	httpOptions.Set("timeout", int64(10))
	httpOptions.Set("content", content)

	return options
}

// HTTPOptions returns $options['http'], creating it (or replacing a
// value that is not an array) as PHP's $options['http'][...] = ... does.
func HTTPOptions(options *php.Array) *php.Array {
	if a, ok := options.GetArray("http"); ok {
		return a
	}

	a := php.NewArray()
	options.Set("http", a)

	return a
}

// AppendHeader performs
//
//	if (isset($http['header'])) { $http['header'] = (array) $http['header']; }
//	$http['header'][] = $header;
func AppendHeader(httpOptions *php.Array, header string) {
	headers, _ := httpOptions.Get("header")
	var list *php.Array
	switch h := headers.(type) {
	case *php.Array:
		list = h
	case *php.Object:
		list = h.ToArray()
		httpOptions.Set("header", list)
	default:
		list = php.NewArray()
		if headers != nil {
			list.Append(headers)
		}
		httpOptions.Set("header", list)
	}

	list.Append(header)
}
