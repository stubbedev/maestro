package command

import "github.com/stubbedev/maestro/internal/composer"

// composerFactory is the type behind Composer's static Factory methods.
type composerFactory = composer.Factory

// composerFile is Factory::getComposerFile().
func composerFile() (string, error) { return composer.GetComposerFile() }
