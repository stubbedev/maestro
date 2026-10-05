package http

import "testing"

// Test doubles exported to the external test package (http_test), which
// can use httpmock without an import cycle.

type (
	FakeConfig = fakeConfig
	FakeSource = fakeSource
	IOMock     = ioMock
	NoProcess  = noProcess
)

func NewFakeConfig(values map[string]any) *FakeConfig { return newFakeConfig(values) }

func NewIOMock(t *testing.T) *IOMock { return newIOMock(t) }

func List(values ...string) any { return list(values...) }

func (c *fakeConfig) AuthCalls() int { return c.authCalls }

func (c *fakeConfig) SourceCalls() int { return c.sourceCalls }

func (c *fakeConfig) Auth() *fakeSource {
	if c.auth == nil {
		c.auth = &fakeSource{name: "auth.json"}
	}

	return c.auth
}

func (c *fakeConfig) Source() *fakeSource {
	if c.source == nil {
		c.source = &fakeSource{name: "composer.json"}
	}

	return c.source
}

func (c *fakeConfig) Gets(key string) int { return c.gets[key] }

func (s *fakeSource) Added() []string { return s.added }

func (s *fakeSource) Removed() []string { return s.removed }

func (s *fakeSource) NameReads() int { return s.nameRead }

func (m *ioMock) Expects(expectations []map[string]any, strict bool) { m.expects(expectations, strict) }

func (m *ioMock) AssertComplete(t *testing.T) { m.assertComplete(t) }

func (p *noProcess) Calls() []string { return p.calls }
