//go:build !unix

package php

import "os"

// getwd is the working directory; Windows has no logical $PWD to avoid.
func getwd() (string, error) { return os.Getwd() }
