package testutil

import (
	"regexp"
	"strings"
)

// OracleRun is what one run of the errors oracle's scenario
// (internal/command/testdata/errors) depends on besides the scenario: the
// places NormalizeOracle replaces with placeholders. errors.sh normalises
// Composer's runs through tools/oracle/errors/normalize, which calls
// NormalizeOracle, and errorstest maestro's runs, so both are normalised
// by this one definition.
type OracleRun struct {
	// Dir is the run's directory (@DIR@), holding the working directory,
	// COMPOSER_HOME and the cache.
	Dir string
	// Server is the local HTTP server's host and port (@SERVER@).
	Server string
	// Composer is the root of the reference Composer's sources
	// (@COMPOSER@). Empty for maestro's runs.
	Composer string
}

// MaestroDiagnoseLine is the line maestro's diagnose adds after
// Composer's version line to say which tool it diagnosed
// (docs/PORTING.md): comparisons with Composer's report leave it out.
var MaestroDiagnoseLine = regexp.MustCompile(`(?m)^Maestro version: .*\n`)

// oracleRules are what differs between runs of a scenario or between
// machines, in the order applied: random names, timings and counts that
// depend on the machine's php, and the paths of the machine's files and
// tools. Goldens hold placeholders for them, so a golden is the same
// whichever machine records it.
var oracleRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	// Cache::gcIsNecessary's random collection (COMPOSER_TEST_SUITE=1 also
	// keeps it from running: it creates the files cache directory).
	{regexp.MustCompile(`(?m)^Running cache garbage collection\n`), ""},
	// the machine in the -vvv "Running ... with PHP" line
	{regexp.MustCompile(`(?m)^(Running [^ \n]+ \([^)\n]*\) with PHP ).* on .*$`), "${1}@PHP@ on @OS@"},
	// temporary archive directories and downloaded dists' random names
	{regexp.MustCompile(`/tmp/composer_archive[0-9a-f]+`), "/tmp/composer_archive@RAND@"},
	{regexp.MustCompile(`/tmp-[0-9a-f]{32}\.`), "/tmp-@RAND@."},
	{regexp.MustCompile(`(?m)^(Memory usage: )[0-9.]+MiB \(peak: [0-9.]+MiB\), time: [0-9.]+s$`), "${1}@PROFILE@"},
	// the pool and rule counts include one platform package per loaded
	// extension
	{regexp.MustCompile(`(?m)^(Analyzed )[0-9]+( (packages|rules) to resolve dependencies)$`), "${1}@N@${2}"},
	{regexp.MustCompile(`(?m)^(Dependency resolution completed in )[0-9.]+( seconds)$`), "${1}@TIME@${2}"},
	{regexp.MustCompile(`(?m)^(Pool optimizer completed in )[0-9.]+( seconds)$`), "${1}@TIME@${2}"},
	{regexp.MustCompile(`(?m)^(Found )[0-9]+( package versions referenced in your dependency graph\. )[0-9]+ \([0-9]+%\)( were optimized away\.)$`), "${1}@N@${2}@N@${3}"},
	{regexp.MustCompile(`(but your php version \()[^)\n]*(\) does not satisfy)`), "${1}@PHPVERSION@${2}"},
	// The CA bundle probe: which of the system locations exist, and the
	// bundle found.
	{regexp.MustCompile(`(?m)^Checked (?:CA file|directory|file or directory) [^\n]* (?:does not exist or it is not a (?:file|directory)|is not readable)\.\n`), ""},
	{regexp.MustCompile(`(?m)^(Checked CA file )[^\n]*(: (?:valid|invalid))$`), "${1}@CAFILE@${2}"},
	// the php.ini files of the solver's missing-extension hint
	{regexp.MustCompile(`(To enable extensions, verify that they are enabled in your \.ini files:\n)(?:    - [^\n]*\n)+`), "${1}    - @PHPINI@\n"},
	// the archive tools Composer finds on the PATH
	{regexp.MustCompile(`(^|[ '\n])/[^ '\n]*/(unzip|7z|7za|7zz)([ '\n]|$)`), "${1}@BIN@/${2}${3}"},
	// curl's connection time, as CompactMessage has it
	{regexp.MustCompile(`( after )[0-9]+( ms: )`), "${1}0${2}"},
	// diagnose's description of the machine's php and tools, and the
	// tool it names
	{MaestroDiagnoseLine, ""},
	{regexp.MustCompile(`(?m)^(PHP version: )[0-9][^ \n]*$`), "${1}@PHPVERSION@"},
	{regexp.MustCompile(`(?m)^(PHP version: [^\n]* - Package overridden via config\.platform, actual: )[^\n]*$`), "${1}@PHPVERSION@"},
	{regexp.MustCompile(`(?m)^(PHP binary path|OpenSSL version|curl version|zip): .*$`), "${1}: @MACHINE@"},
	{regexp.MustCompile(`(?m)^(Checking git settings: OK git version ).*$`), "${1}@GITVERSION@"},
}

// NormalizeOracle is a scenario run's output (or a file it left) as the
// goldens hold it: run's paths, what differs between runs and machines
// (oracleRules) and the banner (NormalizeBanner) replaced with
// placeholders.
func NormalizeOracle(s string, run OracleRun) string {
	s = strings.ReplaceAll(s, run.Dir, "@DIR@")
	s = strings.ReplaceAll(s, run.Server, "@SERVER@")
	if run.Composer != "" {
		s = strings.ReplaceAll(s, run.Composer+"/", "@COMPOSER@/")
	}
	for _, r := range oracleRules {
		s = r.re.ReplaceAllString(s, r.repl)
	}

	return NormalizeBanner(s)
}
