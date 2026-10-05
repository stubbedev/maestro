// Throw sites of the TransportExceptions this package constructs.

package http

import (
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

// transportError is new TransportException($message, $code) constructed at
// site.
func transportError(site phperr.Site, message string, code int) *util.TransportError {
	e := util.NewTransportError(message, code)
	e.Site = site

	return e
}

// maxFileSizeError is new MaxFileSizeExceededException($message)
// constructed at site.
func maxFileSizeError(site phperr.Site, message string) *util.MaxFileSizeExceededError {
	e := util.NewMaxFileSizeExceededError(message)
	e.Site = site

	return e
}
