package platform

import (
	"encoding/json"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/util/fsstate"
)

// fakeProbeOutput is what probe.php prints for a php of version, which
// mapped these files.
func fakeProbeOutput(t *testing.T, version string, mapped ...string) []byte {
	t.Helper()

	return fakeProbeOutputWith(t, nil, version, mapped...)
}

// fakeProbeOutputWith is fakeProbeOutput with the fields of extra too.
func fakeProbeOutputWith(t *testing.T, extra map[string]any, version string, mapped ...string) []byte {
	t.Helper()

	result := map[string]any{
		"format":       probeFormat,
		"constants":    map[string]any{"PHP_VERSION": version, "PHP_VERSION_ID": 80425},
		"mapped_files": mapped,
	}
	maps.Copy(result, extra)

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}

	return append([]byte(probeMarker), data...)
}

// trustedStart is a probe start time the files a test just wrote are
// old enough for.
func trustedStart() time.Time {
	return time.Now().Add(fsstate.DefaultMargin + time.Second)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}
