//go:build race

package semver

// raceEnabled is set in -race builds, where single-goroutine tests that
// only check results can sample their inputs.
const raceEnabled = true
