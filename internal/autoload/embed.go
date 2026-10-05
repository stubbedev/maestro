// The PHP files and license Composer ships with its autoloader, embedded
// verbatim from Composer 2.10.3: src/Composer/Autoload/ClassLoader.php,
// src/Composer/InstalledVersions.php and LICENSE (MIT, Copyright (c) Nils
// Adermann, Jordi Boggiano).

package autoload

import _ "embed"

// ClassLoaderPHP is Composer's ClassLoader.php, which dump() copies to
// vendor/composer/ClassLoader.php.
//
//go:embed res/ClassLoader.php
var ClassLoaderPHP string

// InstalledVersionsPHP is Composer's InstalledVersions.php, which
// FilesystemRepository::write copies to vendor/composer/InstalledVersions.php.
//
//go:embed res/InstalledVersions.php
var InstalledVersionsPHP string

// License is Composer's LICENSE as composer.phar holds it, which dump()
// copies to vendor/composer/LICENSE: the phar Compiler adds LICENSE files
// with a line feed before and after their content (Compiler::addFile), so
// the copy differs from the source repository's file.
var License = "\n" + licenseSource + "\n"

//go:embed res/LICENSE
var licenseSource string
