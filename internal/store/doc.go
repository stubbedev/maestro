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
//	                                  by the SHA-256 of its dist identity,
//	                                  or per stored directory tree (below)
//	tmp/                              files being written, renamed into place
//	lock                              shared by writers, exclusive for Prune
//
// A release's dist identity is its package name, dist type, URL, reference
// and shasum, plus archive.Rules: the version of the extraction rules and,
// for zip, the locale class unzip decodes names in. The index lists every
// entry of the package tree (path, kind, mode before or after the umask,
// symlink target, size and content hash); it never depends on the umask, so
// one index serves every project. Objects do: an object carries the final
// permission bits under the importing process's umask, so that a hardlink
// to it is exactly what Composer would have written (objects for another
// umask are created from the content on first use).
//
// Every write the store makes is atomic. An object is written once into
// tmp/ and renamed into place without replacing (two processes inserting
// the same content both succeed, one rename wins, the other's copy is
// dropped); an index is renamed into place last, so a present index only
// ever names objects already written. A package directory is assembled in a
// sibling of its destination and renamed onto it.
//
// # Directory trees
//
// InsertDir stores a directory as it is on disk under an id the caller
// derives from everything the tree depends on; LookupNamed finds it again.
// internal/downloader/vcs keeps git checkouts cloned from the mirror cache
// this way (deviation 1 extended to source installs). The index is named
// by the SHA-256 of its own magic string, the umask and the id, so it never
// collides with a dist's; its entries record the permission bits found on
// disk (Umask unset), which is why the umask is part of the name. Trees
// share objects, Prune, Verify and Stats with releases. Checkouts are
// imported unshared (people edit them), never hard-linked.
//
// # Importing
//
// MAESTRO_PACKAGE_IMPORT_METHOD selects how files get into a package
// directory. auto (the default, pnpm's) clones by reflink (FICLONE on
// Linux, clonefile on macOS) where the filesystem can, else hardlinks the
// object, else copies (copy_file_range where available), chosen once per
// destination device and run; clone, hardlink and copy use that method
// only and fail where the filesystem refuses it. A file the hardlink would
// give the wrong mode (one its owner may not read) and a file at the
// filesystem's hardlink limit are copied instead.
//
// On Windows there is no umask and no reflink: auto hardlinks on NTFS,
// a file's mode is only its read-only attribute (which hardlinks share, so
// a Chmod that changes it unshares the file first), stamps compare that
// attribute instead of the permission bits, and the lock is LockFileEx's.
//
// Packages that rewrite their own files in place are imported unshared
// (ImportOptions.Unshared), by clone or copy, never by hardlink, whatever
// the method: internal/downloader asks for it for Composer plugins
// (composer-plugin and composer-installer packages), since
// phpstan/extension-installer, infection/extension-installer and others
// rewrite their GeneratedConfig.php with file_put_contents, which through a
// hardlink would put one project's generated configuration into every other
// project.
//
// Directories are created once each, parents first, with their final modes
// applied deepest first at the end (so a read-only directory can still be
// filled); a setgid bit the package directory inherits from its parent is
// kept on every directory, as unzip keeps it. Symlinks are created as
// recorded and never followed. File modification times are not preserved:
// every imported file carries its object's stamp (below) instead, which
// hardlinks share and clones and copies are given, so that a later run
// can tell from one stat that a package file still holds the release's
// content (Entry.ModTime; the autoload dump uses it instead of reading
// the file).
//
// Imports running at once share a few goroutines between them
// (Options.Workers, by default GOMAXPROCS but at most 8): filesystems
// create files no faster when more threads contend for their locks, and
// btrfs slower.
//
// # Hard-linked package files
//
// Composer leaves vendor files writable (0644), and objects keep exactly
// the modes Composer gives rather than being made read-only (read-only
// links would make vendor/ differ from Composer's and break tools that
// write in place). Most tools replace files (write a new file, rename it
// over the old one; git, rsync, patch, composer-patches), which leaves the
// shared inode untouched. A tool that writes in place into a hard-linked
// package file changes the inode it shares with the store and with every
// other project linked to it: those projects see the edit until their next
// install of that release. That is the risk pnpm accepts for the disk it
// saves, and maestro accepts it too; MAESTRO_PACKAGE_IMPORT_METHOD=copy (or
// a filesystem with reflinks) avoids it. maestro's own in-place change of a
// package file, Composer's chmod of package binaries, goes through Chmod,
// which first gives a hard-linked file an inode of its own (Unshare).
//
// What the store guarantees is that such an edit never spreads further:
// no import after it, in any project, gets the edited content. The store's
// own writes only ever go through a temporary file renamed into place, and
// every object is stamped: its size and mode are known from the index and
// its modification time is derived from its hash (a fixed second between
// 2000 and 2008, nanoseconds zero, never the time anything wrote it). Any
// write to an inode, through whichever name, sets its modification time to
// the time of the write, before the data changes; a chmod changes its mode.
// So each import checks the object against its stamp: a hardlink is checked
// after it was made (one lstat of the new name, so content written before
// the link existed cannot pass), a clone or copy after reading the object
// (an fstat; a write before or during the copy moves the time; macOS's
// clonefile, which works on names, checks before and after), and a file
// that fails is removed again. An object that fails is healed before
// use: its content is hashed while it is copied into a new file that
// replaces it (the edited inode stays with the package files linked to it),
// and when the hash is wrong the object is dropped and the release fails
// with *MissingError. The caller then inserts the release again from the
// dist archive, which internal/downloader keeps in Composer's files cache
// (cache-files-dir) as Composer does, and downloads only when that is gone
// too.
//
// The stamp cannot use the link count (several names are now normal) nor
// the change time (every new or removed link moves it), and the inode
// number says nothing about content. An edit therefore escapes detection
// only if it keeps the size and is followed by setting the modification
// time back to the object's exact stamp second with zero nanoseconds,
// something no editor, installer or patch tool does (a copy of another
// vendor file's time is a different second unless the 28 hash bits the
// second is derived from collide). Verify re-hashes every object for
// anything else, including edits made to the store itself.
//
// # Maintenance
//
// Lookup marks an index as used (its modification time). Prune removes the
// indexes unused for a given age and every object no remaining index
// refers to (package files linked to a removed object keep it); Verify
// re-hashes every object and drops the corrupt ones; Stats reports what the
// store holds and how many bytes it saves, hardlinks included.
package store
