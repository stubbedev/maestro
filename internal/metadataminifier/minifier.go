// Ports composer/metadata-minifier 1.0.1 (src/MetadataMinifier.php).

// Package metadataminifier ports Composer\MetadataMinifier: the diff
// encoding of the version lists in Composer v2 repository metadata
// ("minified": "composer/2.0").
package metadataminifier

import "github.com/stubbedev/maestro/internal/php"

// unset is the value marking a key the version drops.
const unset = "__unset"

// Expand ports MetadataMinifier::expand: the full version arrays of a
// list of minified ones, each holding the changes from the previous
// version.
//
// Every version is a new array, but the values they keep from the
// previous version are shared, as PHP's copy-on-write shares them, so
// callers must not modify nested arrays in place.
func Expand(versions []*php.Array) []*php.Array {
	expanded := make([]*php.Array, 0, len(versions))

	var expandedVersion *php.Array
	for _, versionData := range versions {
		if expandedVersion == nil || expandedVersion.Len() == 0 {
			expandedVersion = versionData
			expanded = append(expanded, expandedVersion)

			continue
		}

		// add any changes from the previous version to the expanded one
		next := php.NewArrayCap(expandedVersion.Len() + versionData.Len())
		for k, v := range expandedVersion.All() {
			next.SetKey(k, v)
		}
		for k, v := range versionData.All() {
			if v == unset {
				next.DeleteKey(k)
			} else {
				next.SetKey(k, v)
			}
		}

		expandedVersion = next
		expanded = append(expanded, expandedVersion)
	}

	return expanded
}

// Minify ports MetadataMinifier::minify: each version reduced to its
// differences from the previous one, with "__unset" for the keys it lacks.
func Minify(versions []*php.Array) []*php.Array {
	minifiedVersions := make([]*php.Array, 0, len(versions))

	var lastKnownVersionData *php.Array
	for _, version := range versions {
		if lastKnownVersionData == nil || lastKnownVersionData.Len() == 0 {
			lastKnownVersionData = version.Clone()
			minifiedVersions = append(minifiedVersions, version)

			continue
		}

		minifiedVersion := php.NewArray()

		// add any changes from the previous version
		for k, v := range version.All() {
			if last, ok := lastKnownVersionData.GetKey(k); !ok || !php.StrictEquals(last, v) {
				minifiedVersion.SetKey(k, v)
				lastKnownVersionData.SetKey(k, v)
			}
		}

		// store any deletions from the previous version for keys missing in current one
		for k := range lastKnownVersionData.All() {
			if !version.Has(k.Value()) {
				minifiedVersion.SetKey(k, unset)
				lastKnownVersionData.DeleteKey(k)
			}
		}

		minifiedVersions = append(minifiedVersions, minifiedVersion)
	}

	return minifiedVersions
}
