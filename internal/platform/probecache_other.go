//go:build !linux && !windows && !darwin

// Ports nothing: probe results are cached only on Linux, Windows and
// macOS, where the probe can tell which files its result depends on
// (/proc/self/maps there, the loaded modules here, the load commands'
// walk there).

package platform

import "time"

func probeCacheKey(string) string { return "" }

func loadProbeCache(string, string) *Snapshot { return nil }

func storeProbeCache(string, string, *Snapshot, []byte, time.Time) {}
