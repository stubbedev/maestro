// Ports src/Composer/Package/Loader/JsonLoader.php.

package loader

import (
	"os"

	"github.com/stubbedev/maestro/internal/json"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/pkg"
	"github.com/stubbedev/maestro/internal/util"
)

// JsonLoader ports Composer\Package\Loader\JsonLoader.
type JsonLoader struct {
	loader LoaderInterface
}

// NewJsonLoader ports JsonLoader::__construct.
func NewJsonLoader(loader LoaderInterface) *JsonLoader { return &JsonLoader{loader: loader} }

// Load ports JsonLoader::load: source is a *json.File, the path of a JSON
// file or a JSON string.
func (l *JsonLoader) Load(source any) (pkg.PackageInterface, error) {
	var (
		config any
		err    error
	)

	switch s := source.(type) {
	case *json.File:
		config, err = s.Read()
	case string:
		if _, statErr := os.Stat(s); statErr == nil {
			var content []byte
			if content, err = os.ReadFile(s); err == nil {
				config, err = json.ParseJSON(string(content), s)
			}
		} else {
			config, err = json.ParseJSON(s, "")
		}
	default:
		return nil, &util.InvalidArgumentError{Message: "JsonLoader: Unknown $json parameter " + php.TypeName(source) +
			". Please report at https://github.com/composer/composer/issues/new."}
	}

	if err != nil {
		return nil, err
	}

	a, ok := config.(*php.Array)
	if !ok {
		return nil, typeError(`Composer\Package\Loader\LoaderInterface::load`, 1, "config", "array", config)
	}

	return l.loader.Load(a, pkg.ClassCompletePackage)
}
