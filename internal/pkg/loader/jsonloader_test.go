package loader_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/pkg/loader"
)

func TestJsonLoader_Load(t *testing.T) {
	l := loader.NewJsonLoader(loader.NewArrayLoader(nil, false))
	content := `{"name": "foo/bar", "version": "1.2.3"}`

	p, err := l.Load(content)
	if err != nil || p.Version() != "1.2.3.0" {
		t.Fatalf("string: %v %v", p, err)
	}

	path := filepath.Join(t.TempDir(), "composer.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	if p, err = l.Load(path); err != nil || p.Name() != "foo/bar" {
		t.Fatalf("path: %v %v", p, err)
	}

	file, err := json.NewFile(path, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if p, err = l.Load(file); err != nil || p.PrettyVersion() != "1.2.3" {
		t.Fatalf("JsonFile: %v %v", p, err)
	}

	_, err = l.Load(5)
	checkException(t, "int", err, "InvalidArgumentException", "JsonLoader: Unknown $json parameter int. Please report at https://github.com/composer/composer/issues/new.")

	if _, err = l.Load(`{"name": `); err == nil {
		t.Error("no error for invalid JSON")
	}
}
