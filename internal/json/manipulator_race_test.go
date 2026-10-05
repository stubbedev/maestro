//go:build race

package json

// manipulatorRace is set in -race builds, where the single-goroutine
// oracle replay samples its cases.
const manipulatorRace = true
