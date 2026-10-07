// Command normalize is the errors oracle's normalisation
// (testutil.NormalizeOracle) for tools/oracle/errors/errors.sh: it copies
// stdin to stdout normalised for the run the flags name, as
// internal/command/errorstest normalises maestro's runs.
//
//	normalize -dir <run directory> -server <host:port> [-composer <sources>]
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/stubbedev/maestro/internal/testutil"
)

func main() {
	var run testutil.OracleRun
	flag.StringVar(&run.Dir, "dir", "", "the run's directory (@DIR@)")
	flag.StringVar(&run.Server, "server", "", "the local HTTP server's host:port (@SERVER@)")
	flag.StringVar(&run.Composer, "composer", "", "the root of the reference Composer's sources (@COMPOSER@)")
	flag.Parse()
	if run.Dir == "" || run.Server == "" || flag.NArg() != 0 {
		flag.Usage()
		os.Exit(2)
	}
	in, err := io.ReadAll(os.Stdin)
	if err == nil {
		_, err = io.WriteString(os.Stdout, testutil.NormalizeOracle(string(in), run))
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "normalize:", err)
		os.Exit(1)
	}
}
