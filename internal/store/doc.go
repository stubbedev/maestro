// Package store is maestro's package store (deliberate deviation 1 in
// docs/PORTING.md): every dist archive is extracted once, its files kept
// once per content and permission bits, and package directories are
// assembled from them by reflink or copy, much as pnpm does.
//
// # Layout
//
// Under the store's root (cache.Store() for maestro's own store):
//
//	files/<2 hex>/<62 hex>[-<mode>]   one object per SHA-256 of the content
//	                                  and permission bits; the suffix is the
//	                                  octal mode, left out for 644
//	index/<2 hex>/<62 hex>            one index per extracted release, named
//	                                  by the SHA-256 of its dist identity
//	tmp/                              files being written, renamed into place
//	lock                              shared by writers, exclusive for Prune
//
// A release's dist identity is its package name, dist type, URL, reference
// and shasum, plus archive.Rules: the version of the extraction rules and,
// for zip, the locale class unzip decodes names in. The index lists every
// entry of the package tree (path, kind, mode before or after the umask,
// symlink target, size and content hash); it never depends on the umask, so
// one index serves every project. An object's file name carries the
// permission bits the file gets under the importing process's umask
// (objects for another umask are created from the content on first use);
// imports set the destination's mode themselves.
//
// Every write is atomic. An object is written once into tmp/ and renamed
// into place without replacing (two processes inserting the same content
// both succeed, one rename wins, the other's copy is dropped); an index is
// renamed into place last, so a present index only ever names objects
// already written. A package directory is assembled in a sibling of its
// destination and renamed onto it.
//
// # Package files are never shared
//
// Composer leaves vendor files writable (0644) and tools write into them in
// place: phpstan/extension-installer and infection/extension-installer
// rewrite their own GeneratedConfig.php with file_put_contents,
// cweagans/composer-patches and editors change sources. A package file must
// therefore never share its inode with the store or with another project,
// or one project's edit would show in every other: there is no hardlink
// import (pnpm's default). Files are reflink clones, which share blocks
// copy-on-write but are independent files, or plain copies.
//
// The store's own objects are only ever written by the store, through a
// temporary file renamed into place, so an object cannot change while
// being cloned or copied. Each import still checks the object it opened
// against its stamp, with the fstat it needs anyway: a regular file of the
// indexed size and mode, a modification time derived from its hash (a fixed
// second between 2000 and 2008, never the time anything wrote it) and a
// link count of one. An object that fails it (changed by hand, or linked
// into a package directory by an earlier maestro, which imported by
// hardlink, so writable through that name) is healed before use: its
// content is hashed while it is copied into a new file that replaces it,
// and when the hash is wrong the object is dropped and the release fails
// with *MissingError. The caller then inserts the release again from the
// dist archive, which internal/downloader keeps in Composer's files cache
// (cache-files-dir) as Composer does, and downloads only when that is gone
// too. So no edit made in one package directory can reach the store or
// another project, whatever runs concurrently; Verify re-hashes everything
// for edits made to the store itself.
//
// # Importing
//
// Files are imported by reflink (FICLONE on Linux, clonefile on macOS),
// else by copy (copy_file_range where available), chosen once per
// destination device and run (MAESTRO_PACKAGE_IMPORT_METHOD=auto), or by
// one method only (clone or copy), failing where it is unsupported.
// Directories are created once each, parents first, with their final modes
// applied deepest first at the end (so a read-only directory can still be
// filled); a setgid bit the package directory inherits from its parent is
// kept on every directory, as unzip keeps it. Symlinks are created as
// recorded and never followed. File modification times are not preserved.
//
// # Maintenance
//
// Lookup marks an index as used (its modification time). Prune removes the
// indexes unused for a given age and every object no remaining index
// refers to; Verify re-hashes every object and drops the corrupt ones;
// Stats reports what the store holds and how many bytes it saves.
package store
