// Package archive extracts Composer dist archives natively (deliberate
// deviation 2 in docs/PORTING.md), reproducing exactly the tree Composer's
// preferred extractor leaves behind on Unix:
//
//   - zip  (ZipDownloader):   Info-ZIP UnZip 6.0 run as `unzip -qq <file> -d <dir>`
//     with stdin at EOF, as Symfony Process runs it;
//   - tar  (TarDownloader):   PharData::extractTo($dir, null, true) for
//     tar, tar.gz and tar.bz2, as PHP 8.4's ext/phar implements it;
//   - xz   (XzDownloader):    GNU `tar -xJf <file> -C <dir>` as a non-root user;
//   - gzip (GzipDownloader):  `gzip -cd <file> > <dir>/<name>`;
//
// followed by ArchiveDownloader's rule: when the extraction holds a single
// top-level directory (ignoring .DS_Store), that directory is the package.
//
// Nothing is written to disk. Open plans the whole extraction from the
// archive's metadata and returns the resulting tree as a list of entries
// (path, kind, mode, link target, size); ReadFiles then streams the content of
// every regular file. Mode bits are reported before the process umask is
// applied, with a flag on the entries whose mode the extractor masks with the
// umask, so one extraction serves every umask (see Entry).
//
// The extraction is reproduced only where it is exact. Whenever Composer's
// extractor would not succeed cleanly (unzip exiting non-zero makes Composer
// fall back to ZipArchive, whose tree differs), or the archive relies on
// behaviour maestro does not reproduce, Open or ReadFiles fail with an *Error
// wrapping ErrIrreproducible; the caller reports it as Composer reports an
// extraction failure. Archives unzip itself rejects as zip bombs fail with
// ErrBomb, archives beyond maestro's safety limits with ErrLimit, and
// unreadable archives with ErrCorrupt.
//
// Every path in the result is relative, slash-separated, free of empty, "."
// and ".." components and of NUL bytes, and no entry lies below a symlink or
// a file: a hostile archive cannot describe anything outside the package
// directory. Sizes declared by the archive are never trusted: content is read
// through readers that fail as soon as more (or other) bytes arrive than the
// plan promised.
package archive
