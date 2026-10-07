//go:build !linux

// Ports nothing: probe results are cached only on Linux, where the probe
// can tell which files its result depends on (/proc/self/maps).

package platform

import "time"

func probeCacheKey(string) string { return "" }

func loadProbeCache(string, string) *Snapshot { return nil }

func storeProbeCache(string, string, *Snapshot, []byte, time.Time) {}
