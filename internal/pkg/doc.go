// Package pkg ports Composer\Package (src/Composer/Package): the package
// classes, Link, Composer's VersionParser subclass and
// Composer\Util\PackageSorter and PackageInfo. The loaders, the dumper and
// the version helpers live in the loader, dumper, version and comparer
// subpackages.
//
// # Class hierarchy
//
// PHP's classes map to Go types and interfaces as follows:
//
//	PackageInterface          PackageInterface
//	CompletePackageInterface  CompletePackageInterface
//	RootPackageInterface      RootPackageInterface
//	Package                   *Package
//	CompletePackage           *CompletePackage (embeds Package)
//	RootPackage               *RootPackage (embeds CompletePackage)
//	AliasPackage              *AliasPackage
//	CompleteAliasPackage      *CompleteAliasPackage (embeds AliasPackage)
//	RootAliasPackage          *RootAliasPackage (embeds CompleteAliasPackage)
//
// `$p instanceof AliasPackage` is a type assertion to Alias, `instanceof
// Package` is AsPackage and `instanceof CompletePackage` is
// AsCompletePackage. Class returns the concrete PHP class name.
//
// # Values
//
// PHP's ?string is NullString. Free-form arrays (extra, autoload, scripts,
// support, ...) are *php.Array. Getters never return a nil array for a PHP
// array (an unset one comes back as a fresh empty array); nil only stands
// for null where PHP allows it (source and dist mirrors, php-ext). Like
// every *php.Array, the arrays returned by getters and given to setters
// are shared, not copied: treat them as read-only, and Clone before
// modifying, where PHP would have copied.
//
// Link maps (requires, conflicts, ...) are Links, an immutable ordered map
// from PHP array key to *Link.
//
// Every setter increments the package's Rev, so callers (the plugin shim)
// can tell whether a package changed. No state is shared between
// packages, loaders or parsers beyond what the caller passes in.
package pkg
