// Ports src/Composer/Config/ConfigSourceInterface.php.

package config

// ConfigSource is Composer\Config\ConfigSourceInterface: a configuration
// file settings can be written to. Values are PHP values (internal/php);
// a repository config is a *php.Array or false.
type ConfigSource interface {
	// AddRepository adds a repository, at the end unless append is false.
	AddRepository(name string, config any, append bool) error
	// InsertRepository inserts a repository before (offset 0) or after
	// (offset 1) the repository named referenceName.
	InsertRepository(name string, config any, referenceName string, offset int) error
	SetRepositoryURL(name, url string) error
	RemoveRepository(name string) error
	AddConfigSetting(name string, value any) error
	RemoveConfigSetting(name string) error
	AddProperty(name string, value any) error
	RemoveProperty(name string) error
	AddLink(typ, name, value string) error
	RemoveLink(typ, name string) error
	// Name is getName(): the source's file name.
	Name() string
}
