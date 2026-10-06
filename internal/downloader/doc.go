// Package downloader ports Composer\Downloader except the VCS downloaders
// (internal/downloader/vcs): DownloadManager, FileDownloader,
// ArchiveDownloader and its zip, tar, xz, gzip, rar and phar variants, and
// PathDownloader.
//
// # Archives and the package store
//
// Deviations 1 and 2 of docs/PORTING.md apply to the zip, tar, xz and gzip
// downloaders. Their archives are extracted natively (internal/archive)
// into the package store (internal/store), and package directories are
// materialized from it. Composer's files cache (cache-files-dir) keeps the
// archives exactly as Composer keeps them (same keys, checksum check,
// cache-read-only, garbage collection by cache-files-ttl and
// cache-files-maxsize), and it alone decides between a cache hit and a
// download, so the output is Composer's:
//
//   - A cache hit prints "Loading ... from cache". When the store holds
//     the release (and may be read: a files cache with cache-files-ttl > 0
//     and a usable directory), the archive is only opened, not copied, and
//     the package is materialized from the store into a staging directory
//     under vendor/composer/ while the other downloads run. Should the
//     store have lost objects of the release, it is healed from that open
//     archive (copied to the temporary file, as copyTo would have, and
//     extracted): the network is never used for a cached archive.
//     POST_FILE_DOWNLOAD names the temporary file, which then does not
//     exist unless the store was healed. When the
//     store lacks the release, the archive is copied and extracted as
//     after a download.
//   - A miss downloads the archive as Composer does, verifies it against
//     the dist shasum, copies it into the files cache, and inserts it into
//     the store (unless the store holds the release already: an archive
//     the cache's garbage collection removed) or, when the shared store
//     must not be written (no files cache, or a read-only one), into a
//     temporary store beside vendor/, and materializes the package into
//     the staging directory.
//
// Packages are materialized with the store's import method (pnpm's auto:
// reflink, else hardlink, else copy), except Composer plugins
// (composer-plugin, composer-installer), whose files are never hard-linked:
// plugins rewrite their own files in place (internal/store's documentation
// says why and what the store guarantees about hard-linked files).
//
// Install then empties the target and renames the staging directory onto
// it (or merges it in, as ArchiveDownloader does when the target is not
// empty). Extraction failures are reported by Install, after its
// "Extracting archive" line, as Composer reports them.
//
// Rar and phar dists have no native extractor and keep Composer's flow:
// files cache, temporary directory, `unrar`, and `php` for Phar::extractTo.
//
// # Promises and goroutines
//
// Operations return (*Promise, error): error is what PHP throws
// synchronously, the promise what it returns. Promises are util.Promise,
// whose then() callbacks run as React's do: at once for a settled promise,
// else when the loop settles it, on the goroutine driving the loop
// (util.Scheduler). Retries, mirror fallbacks, POST_FILE_DOWNLOAD and all
// output therefore happen on that goroutine, in a deterministic order.
// What runs in parallel is the work behind the promises: HTTP transfers,
// processes, and the extraction of archives into the package store
// (util.Go on the ProcessExecutor's scheduler, which a Loop shares).
//
// The zip downloader's Install settles on a later loop tick, as Composer's
// (its unzip runs asynchronously), so what follows it in the installers
// runs after the other operations of the batch started; tar, xz and gzip
// failures are thrown synchronously, as Composer's extractors do.
package downloader
