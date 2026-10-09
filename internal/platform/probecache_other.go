//go:build !linux && !windows

// Ports nothing: probe results are cached only on Linux and Windows,
// where the probe can tell which files its result depends on
// (/proc/self/maps there, the loaded modules here).

package platform

import "time"

func probeCacheKey(string) string { return "" }

func loadProbeCache(string, string) *Snapshot { return nil }

func storeProbeCache(string, string, *Snapshot, []byte, time.Time) {}
