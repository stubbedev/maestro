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

// License is Composer's LICENSE, which dump() copies to
// vendor/composer/LICENSE.
//
//go:embed res/LICENSE
var License string
