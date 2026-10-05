package downloader

import "sync"

// Metadata is FileDownloader::$downloadMetadata, a static in Composer:
// the size of each package's downloaded dist, by package name, for
// InstallationManager's install notifications. One is shared by the
// downloaders of a Composer instance.
type Metadata struct {
	m  map[string]any
	mu sync.Mutex
}

// NewMetadata returns an empty Metadata.
func NewMetadata() *Metadata {
	return &Metadata{m: map[string]any{}}
}

// Get is FileDownloader::$downloadMetadata[$name]: the file size (int64),
// or the Content-Length header or "?" (string).
func (m *Metadata) Get(name string) (any, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	v, ok := m.m[name]

	return v, ok
}

// Reset is FileDownloader::$downloadMetadata = [].
func (m *Metadata) Reset() {
	m.mu.Lock()
	clear(m.m)
	m.mu.Unlock()
}

func (m *Metadata) set(name string, size any) {
	m.mu.Lock()
	m.m[name] = size
	m.mu.Unlock()
}
