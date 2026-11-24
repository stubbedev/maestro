// Command shimlines checks the plugin shim's `@line` annotations against
// the Composer release maestro ports (docs/PLUGINS.md, "Exception
// traces"): every `// @line N` (Composer's file at the shim file's path
// under src/) or `// @line path:N` (a file relative to Composer's root)
// in internal/plugin/php/src must name the line of Composer's source
// that makes the same call, the same function or method with the same
// first string argument. It prints every annotation that names another
// line, with the line or lines where that call is now, and exits 1.
//
// Usage, from the repository root with .ref/composer checked out
// (`just ref`):
//
//	go run ./tools/shimlines        # or `just shim-lines`
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	ref := flag.String("ref", ".ref/composer", "the checkout of the Composer release maestro ports (internal/upstream)")
	src := flag.String("src", "internal/plugin/php/src", "the shim's hand-written sources")
	flag.Parse()

	drifts, err := check(*ref, *src)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shimlines:", err)
		os.Exit(1)
	}
	for _, d := range drifts {
		fmt.Fprintln(os.Stderr, d)
	}
	if len(drifts) > 0 {
		os.Exit(1)
	}
}
