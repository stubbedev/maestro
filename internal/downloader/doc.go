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
// materialized from it. Composer's dist file cache (cache-files-dir) is
// not used for them; the store takes its place:
//
//   - With a files cache (cache-files-ttl > 0, the cache directory usable),
//     a dist already in the store is not downloaded. Download prints the
//     "Loading ... from cache" line Composer prints for a cache hit, and
//     materializes the package into a staging directory under
//     vendor/composer/ while the other downloads run. If objects went
//     missing from the store, the archive is downloaded after all.
//   - Otherwise (no files cache, or a read-only one for inserts) the
//     archive is downloaded as Composer downloads it, verified against the
//     dist shasum, and inserted into the store (or into a temporary store
//     beside vendor/ when the shared one must not be written) and
//     materialized into the staging directory.
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
