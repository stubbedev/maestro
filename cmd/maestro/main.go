// Command maestro is Composer, natively: a drop-in replacement for the
// composer command, ported from Composer 2.10.3 to Go.
package main

import (
	"fmt"
	"os"
)

// version is stamped by the build; see package.nix.
var version = "dev"

func main() {
	fmt.Fprintf(os.Stderr, "maestro %s: not wired up yet\n", version)
	os.Exit(1)
}
