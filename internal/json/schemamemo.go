// Ports nothing: Composer validates composer.json against its schema on
// every run (deliberate deviation 3, speed).

package json

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// schemaMemoSize is how many documents a schemaMemo remembers.
const schemaMemoSize = 64

// schemaMemo remembers the documents that validated against one of
// Composer's own schemas without a finding: the same binary validates the
// same document the same way, so a run reading an unchanged composer.json
// skips its validation. Only successes are kept, so a document with
// findings is validated, and they are reported, every time. The file
// holds schemaFormat's header, then the SHA-256 of each document, the
// most recent last.
type schemaMemo struct {
	path string

	mu     sync.Mutex
	loaded bool
	sums   [][sha256.Size]byte
}

// schemaMemos is the memo ValidateJSONSchema uses, nil for none.
var schemaMemos atomic.Pointer[schemaMemo]

// UseSchemaMemo has ValidateJSONSchema remember in the file at path the
// documents that validated against Composer's schemas. Without it (tests)
// every document is validated.
func UseSchemaMemo(path string) { schemaMemos.Store(&schemaMemo{path: path}) }

// schemaFormat is the version of the memo, and of the validation and the
// schemas whose successes it remembers.
var schemaFormat = fsstate.Format{Name: "schema-memo", Version: 4}

// schemaKey is what identifies a validation against Composer's schemas:
// the schema (LaxSchema, ...) and the document.
func schemaKey(schema int, encoded string) [sha256.Size]byte {
	return sha256.Sum256([]byte(strconv.Itoa(schema) + "\x00" + encoded))
}

// validated reports whether the document with sum validated before.
func (m *schemaMemo) validated(sum [sha256.Size]byte) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.load()

	return slices.Contains(m.sums, sum)
}

// remember records that the document with sum validated.
func (m *schemaMemo) remember(sum [sha256.Size]byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.load()
	m.sums = append(slices.DeleteFunc(m.sums, func(s [sha256.Size]byte) bool { return s == sum }), sum)
	if len(m.sums) > schemaMemoSize {
		m.sums = m.sums[len(m.sums)-schemaMemoSize:]
	}

	var b bytes.Buffer
	b.WriteString(schemaFormat.Header())
	for _, s := range m.sums {
		b.WriteString(hex.EncodeToString(s[:]))
		b.WriteByte('\n')
	}
	// a memo that cannot be written only costs the next run a validation
	_ = fsstate.WriteAtomic(m.path, b.Bytes())
}

// load reads the file once; one of another format, or malformed, holds
// nothing.
func (m *schemaMemo) load() {
	if m.loaded {
		return
	}
	m.loaded = true

	data, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	rest, ok := bytes.CutPrefix(data, []byte(schemaFormat.Header()))
	if !ok {
		return
	}
	var sums [][sha256.Size]byte
	for s := bufio.NewScanner(bytes.NewReader(rest)); s.Scan(); {
		var sum [sha256.Size]byte
		if n, err := hex.Decode(sum[:], s.Bytes()); err != nil || n != sha256.Size {
			return
		}
		sums = append(sums, sum)
	}
	m.sums = sums
}
