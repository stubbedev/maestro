// Ports nothing: the structures the pool optimizer files its hashes in
// (deliberate deviation 3, speed).

package resolver

import (
	"hash/maphash"
	"sync"
	"sync/atomic"
)

// hashIDs gives each distinct hash string an id, the same id for equal
// strings whichever goroutine asks, so that the optimizer files packages
// by small ids instead of their hash strings (a group hash of a package
// replacing many names is kilobytes long, and symfony's pool has hundreds
// of thousands of them, only thousands distinct). Ids are compared,
// never ordered.
type hashIDs struct {
	seed   maphash.Seed
	next   atomic.Int32
	shards [64]hashShard
}

type hashShard struct {
	sync.Mutex
	ids map[string]int32
}

func newHashIDs() *hashIDs {
	return &hashIDs{seed: maphash.MakeSeed()}
}

// id returns the id of the string b holds.
func (h *hashIDs) id(b []byte) int32 {
	shard := &h.shards[maphash.Bytes(h.seed, b)%uint64(len(h.shards))]
	shard.Lock()
	defer shard.Unlock()

	id, ok := shard.ids[string(b)]
	if !ok {
		if shard.ids == nil {
			shard.ids = map[string]int32{}
		}
		id = h.next.Add(1)
		shard.ids[string(b)] = id
	}

	return id
}

// local returns a cache of h's ids for one goroutine.
func (h *hashIDs) local() *localHashIDs {
	return &localHashIDs{shared: h, ids: map[string]int32{}}
}

// localHashIDs is a goroutine's cache of the ids of a hashIDs.
type localHashIDs struct {
	shared *hashIDs
	ids    map[string]int32
}

// id returns the id of the string b holds.
func (l *localHashIDs) id(b []byte) int32 {
	if id, ok := l.ids[string(b)]; ok {
		return id
	}
	id := l.shared.id(b)
	l.ids[string(b)] = id

	return id
}

// orderedIDs is a map from ids to values that keeps the order the ids
// were first set in, as a PHP array keyed by the hash strings would.
type orderedIDs[V any] struct {
	index map[int32]int
	vals  []V
}

// at returns the value of id, a zero value appended when id is new. The
// pointer is valid until the next call.
func (m *orderedIDs[V]) at(id int32) *V {
	i, ok := m.index[id]
	if !ok {
		if m.index == nil {
			m.index = map[int32]int{}
		}
		i = len(m.vals)
		m.index[id] = i
		var zero V
		m.vals = append(m.vals, zero)
	}

	return &m.vals[i]
}
