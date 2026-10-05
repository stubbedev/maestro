// Package res embeds the JSON schemas of Composer's res/ directory
// (res/composer-schema.json, res/composer-lock-schema.json and
// res/composer-repository-schema.json), copied verbatim from Composer 2.10.3.
package res

import _ "embed"

// URIs under which the schemas are addressed, standing in for the
// file://.../res/*.json paths Composer builds from JsonFile::COMPOSER_SCHEMA_PATH
// and JsonFile::LOCK_SCHEMA_PATH.
const (
	ComposerSchemaURI   = "file:///composer/res/composer-schema.json"
	LockSchemaURI       = "file:///composer/res/composer-lock-schema.json"
	RepositorySchemaURI = "file:///composer/res/composer-repository-schema.json"
)

var (
	//go:embed composer-schema.json
	composerSchema string
	//go:embed composer-lock-schema.json
	lockSchema string
	//go:embed composer-repository-schema.json
	repositorySchema string
)

// ComposerSchema returns res/composer-schema.json.
func ComposerSchema() string { return composerSchema }

// LockSchema returns res/composer-lock-schema.json.
func LockSchema() string { return lockSchema }

// RepositorySchema returns res/composer-repository-schema.json.
func RepositorySchema() string { return repositorySchema }

// Lookup returns the schema addressed by uri (one of the *URI constants,
// without fragment), and whether it is known.
func Lookup(uri string) (string, bool) {
	switch uri {
	case ComposerSchemaURI:
		return composerSchema, true
	case LockSchemaURI:
		return lockSchema, true
	case RepositorySchemaURI:
		return repositorySchema, true
	}
	return "", false
}
