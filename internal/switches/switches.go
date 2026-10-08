// Package switches names the environment variables a developer or CI sets
// to turn on opt-in tests, tune them, and profile a maestro_profile build.
// docs/PORTING.md ("Opt-in test switches", "Profiling") documents each
// one; a test here fails when the documentation and these constants
// disagree, or when code reads a switch by its literal name instead of
// its constant.
//
// It ports nothing. maestro's own runtime variables (MAESTRO_CACHE_DIR,
// MAESTRO_PACKAGE_IMPORT_METHOD, the plugin shim's MAESTRO_IPC, ...) and
// variables one test sets for itself are not switches.
package switches

import "os"

// Opt-in tests: each turns on tests that are skipped without it.
const (
	// PHPTests ("1") runs the tests that run php: scripts, plugins,
	// platform detection.
	PHPTests = "MAESTRO_PHP_TESTS"
	// E2E ("1") runs the end-to-end comparison with Composer and the
	// functional fixtures (php, git, unzip and the network).
	E2E = "MAESTRO_E2E"
	// NetworkTests ("1") runs the tests that install real packages from
	// GitHub.
	NetworkTests = "MAESTRO_NETWORK_TESTS"
	// OracleLive ("1") runs the classmap oracles against php live.
	OracleLive = "MAESTRO_ORACLE_LIVE"
	// PerfBudgets ("1") fails the plugin runtime's timing tests when a
	// timing misses its budget; without it they only log it.
	PerfBudgets = "MAESTRO_PERF_BUDGETS"
	// SyscallBudgets ("1") runs the tests that count, under strace, the
	// system calls an install makes per package file, and fail when the
	// count exceeds its budget.
	SyscallBudgets = "MAESTRO_SYSCALL_BUDGETS"
	// UpdateFormats ("1") has internal/cache's TestOwnFormats record the
	// fingerprint of a cache format version not recorded yet.
	UpdateFormats = "MAESTRO_UPDATE_FORMATS"
	// UpdateLogo ("1") has internal/ui's TestLogoSVG redraw
	// docs/assets/maestro-logo.svg from the console's wordmark.
	UpdateLogo = "MAESTRO_UPDATE_LOGO"
)

// End-to-end knobs, read with E2E on.
const (
	// E2EBin is a maestro binary to compare instead of one built from the
	// tree.
	E2EBin = "MAESTRO_E2E_BIN"
	// E2EKeep is a directory the scenarios run in and are kept in, instead
	// of a temporary directory removed afterwards.
	E2EKeep = "MAESTRO_E2E_KEEP"
	// E2EWarm "0" runs the cold phase only.
	E2EWarm = "MAESTRO_E2E_WARM"
	// E2EPlugins is a comma-separated list of the plugin scenarios to run;
	// the others skip.
	E2EPlugins = "MAESTRO_E2E_PLUGINS"
	// E2EReport is a file TestE2E writes its wall-time table to.
	E2EReport = "MAESTRO_E2E_REPORT"
	// E2EPrivateApp is the checkout of a private Laravel application (with
	// its composer.lock) for the private-app scenario.
	E2EPrivateApp = "MAESTRO_E2E_PRIVATE_APP"
)

// Test inputs.
const (
	// OracleSeed and OracleCount are the seed (default 42) and number of
	// cases (default 50000) of the classmap random oracle with OracleLive.
	OracleSeed  = "MAESTRO_ORACLE_SEED"
	OracleCount = "MAESTRO_ORACLE_COUNT"
	// OracleVersions is a directory of another versions set
	// tools/oracle/classmap/versions.sh wrote (default testdata/oracle).
	OracleVersions = "MAESTRO_ORACLE_VERSIONS"
	// TestDists is the directory of real dists tools/fetchdists fetched
	// for the store's differential test (default
	// <user cache dir>/maestro-test-dists).
	TestDists = "MAESTRO_TEST_DISTS"
	// TestUnzip and TestTar are the reference unzip and tar the archive
	// tests compare with (default unzip, and tar then gtar, from PATH).
	TestUnzip = "MAESTRO_TEST_UNZIP"
	TestTar   = "MAESTRO_TEST_TAR"
	// P2Dirs is a path list of more directories of cached Packagist p2
	// files (provider-*.json) the decoded metadata cache's test checks,
	// besides Composer's cache directories.
	P2Dirs = "MAESTRO_P2_DIRS"
)

// Profiling, in a binary built with -tags maestro_profile only: each names
// the file to write.
const (
	// CPUProfile is a CPU profile (go tool pprof).
	CPUProfile = "MAESTRO_CPUPROFILE"
	// MemProfile is an allocation profile, sampled every 4 KiB, written
	// when the run ends.
	MemProfile = "MAESTRO_MEMPROFILE"
	// Trace is an execution trace (go tool trace).
	Trace = "MAESTRO_TRACE"
)

// On reports whether the switch name is set to "1".
func On(name string) bool {
	return os.Getenv(name) == "1"
}
