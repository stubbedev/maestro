// Ports nothing: cached metadata files kept decoded between runs, so that
// a run reads them back instead of decoding their JSON again (deliberate
// deviation 3, speed).

package composerrepo

import (
	"errors"
	"path/filepath"

	"github.com/stubbedev/maestro/internal/cache"
	"github.com/stubbedev/maestro/internal/php"
)

// decodedP2 keeps the decoded metadata files; see UseDecodedCache.
var decodedP2 = cache.NewDecoded(decodedMagic, decodedMinSize, 0)

// UseDecodedCache keeps, under root (cache.DecodedMetadata), the decoded
// form of the cached metadata files the repositories decode, for later
// runs (cache.Decoded: each cached file has one slot); "" keeps none (the
// default). Call it before repositories load metadata.
func UseDecodedCache(root string) {
	dir := root
	if root != "" {
		dir = filepath.Join(root, decodedVersion)
	}
	decodedP2.Use(dir)
}

// decodedVersion is the directory, under the root, of the slots of the
// current form; it changes with decodedMagic.
const decodedVersion = "v2"

// decodedMagic starts a decoded file; its version changes with the slot's
// form (cache.Decoded) and the binary form.
const decodedMagic = "maestro-p2-v2\n"

// decodedMinSize is the size under which JSON is decoded at once: reading
// a small file back costs more than decoding it.
const decodedMinSize = 4096

// errNotArray is a metadata file that does not decode to an array.
var errNotArray = errors.New("not an array")

// decodeCached is decodeArray(json) for json, the contents of the file
// cached under cacheKey, read back from the decoded cache when it holds
// them. Else store (non-nil) stores the decoded file there; call it before
// anything may change the array.
func (r *ComposerRepository) decodeCached(cacheKey, json string) (data *php.Array, store func()) {
	if r.cache == nil || r.cache.Root() == "" {
		return decodeArray(json), nil
	}
	v, store, err := decodedP2.Decode(r.cache.Root()+"\x00"+cacheKey, r.cache.OriginOf(cacheKey, json), json, func(json string) (any, error) {
		if a := decodeArray(json); a != nil {
			return a, nil
		}

		return nil, errNotArray
	})
	if a, ok := v.(*php.Array); ok && err == nil {
		return a, store
	}

	return nil, nil
}
