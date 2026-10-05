// Package store is maestro's package store (deliberate deviation 1 in
// docs/PORTING.md): every dist archive is extracted once, its files kept
// once per content and permission bits, and package directories are
// assembled from them by reflink, hardlink or copy, as pnpm does.
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
// one index serves every project. Objects do: a file's object carries the
// final permission bits under the importing process's umask, so that a
// hardlink to it is exactly what Composer would have written. Objects for
// another umask are created from the content on first use.
//
// Every write is atomic. An object is written once into tmp/ and renamed
// into place without replacing (two processes inserting the same content
// both succeed, one rename wins, the other's copy is dropped); an index is
// renamed into place last, so a present index only ever names objects
// already written. A package directory is assembled in a sibling of its
// destination and renamed onto it.
//
// # Writable vendor files
//
// Composer leaves vendor files writable (0644) and tools such as
// cweagans/composer-patches change them. Objects keep exactly the modes
// Composer gives, like pnpm's store, rather than being made read-only:
// read-only hardlinks would make vendor/ differ from Composer's and break
// tools that write in place. Most tools replace files (write a new file,
// rename it over the old one), which leaves the shared object untouched.
// A tool that writes in place into a file linked to an object changes the object for
// every project linked to it, so every object is stamped: its modification
// time is derived from its hash (a fixed second between 2000 and 2008,
// never the time anything wrote it) and its size and mode are known from
// the index. Before an object is linked, cloned or copied again it is
// checked against that stamp with one stat; an object that no longer
// matches is re-hashed, replaced by a fresh copy (a new inode) when its
// content is intact, and dropped otherwise, so modified content never
// spreads to another project. maestro's own in-place changes go through
// Chmod and Unshare, which first give a shared file its own inode.
// Reflinks and copies are independent files and need none of this; where
// that guarantee matters more than speed, MAESTRO_PACKAGE_IMPORT_METHOD
// selects copy.
//
// # Importing
//
// Files are imported by reflink (FICLONE on Linux, clonefile on macOS),
// then hardlink, then copy, chosen once per destination device and run
// (MAESTRO_PACKAGE_IMPORT_METHOD=auto), or by one method only (clone,
// hardlink or copy), failing where it is unsupported. A file at the
// filesystem's hardlink limit is copied instead. Directories are created
// once each, parents first, with their final modes applied deepest first
// at the end (so a read-only directory can still be filled); a setgid bit
// the package directory inherits from its parent is kept on every
// directory, as unzip keeps it. Symlinks are created as recorded and never
// followed. File modification times are not preserved.
//
// # Maintenance
//
// Lookup marks an index as used (its modification time). Prune removes the
// indexes unused for a given age and every object no remaining index
// refers to; Verify re-hashes every object and drops the corrupt ones;
// Stats reports what the store holds and how many bytes it saves.
package store
