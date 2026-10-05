// Ports src/Composer/FilterList/Source/SourceValidator.php and
// src/Composer/FilterList/Source/UrlSource.php.

// Package source ports Composer\FilterList\Source: the URL sources of
// custom policy lists.
package source

import (
	"strings"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// URLSource is UrlSource: a filter list fetched from a URL.
type URLSource struct {
	ListName string
	URL      string
}

// Validate ports SourceValidator::validate: the source configuration of
// the policy list listName, or a *util.RuntimeError.
func Validate(listName string, source *php.Array) (*URLSource, error) {
	typ, ok := source.Get("type")
	if !ok || typ == nil {
		return nil, &util.RuntimeError{Message: `Source configuration is missing the "type" field.`, Site: phperr.At("SourceValidator.php", 28)}
	}

	if typ == "url" {
		return validateURLSource(listName, source)
	}

	return nil, &util.RuntimeError{Message: `Unsupported source type "` + php.ToString(typ) + `". Only "url" is currently supported.`, Site: phperr.At("SourceValidator.php", 35)}
}

func validateURLSource(listName string, source *php.Array) (*URLSource, error) {
	url, ok := source.GetString("url")
	if !ok {
		return nil, &util.RuntimeError{Message: `Source configuration is missing a string "url" field.`, Site: phperr.At("SourceValidator.php", 44)}
	}
	if !strings.HasPrefix(url, "https://") {
		return nil, &util.RuntimeError{Message: `Source URL for policy list "` + listName + `" must start with "https://"; got "` + url + `".`, Site: phperr.At("SourceValidator.php", 47)}
	}

	return &URLSource{ListName: listName, URL: url}, nil
}
