// Package archiver ports Composer\Package\Archiver: ArchiveManager, the
// PharArchiver and ZipArchiver, and ArchivableFilesFinder with its exclude
// filters, which internal/downloader's PathDownloader also mirrors path
// repositories with.
//
// # Archive formats
//
// Composer writes archives through PHP extensions; this package writes
// them natively (docs/PORTING.md, deviation 2) and reproduces their output:
//
//   - PharArchiver (PharData): the tar and zip archives are byte for byte
//     what PHP 8.4's phar extension writes, except for the entry
//     timestamps, which are the time of archiving in both. That covers
//     PharData's quirks: entries named after the real path of each file
//     (a symbolic link becomes a copy of its target, under the target's
//     name), files in the magic .phar directory silently left out, its
//     entry name checks and their BadMethodCallException messages, a "?"
//     ending an entry name, the tar name/prefix split and its
//     PharException for longer names, file permissions masked to 0777,
//     empty directories after the files, the extension rules of
//     PharData's file name (an extension whose first "z" starts "zip"
//     makes a zip), and compress() replacing the last extension. tar.gz is
//     gzip with zlib's header fields but Go's deflate encoder, so the
//     compressed bytes differ from zlib's while the tar inside is
//     identical; tar.bz2 likewise uses another bzip2 encoder. PHP builds
//     without bz2 cannot write tar.bz2 ("Can not compress to tar.bz2
//     format"); this port always can.
//   - ZipArchiver (ZipArchive, libzip 1.11): entry names, order, flags,
//     versions, external attributes, times and contents are libzip's.
//     Files are deflated at level 9 by Go's encoder, so compressed data
//     (and, for barely compressible files, the choice to store instead)
//     can differ from zlib's.
//
// Neither archiver adds to an archive that already exists at the target,
// as PharData (for the .tar of a tar.gz/tar.bz2 target) and ZipArchive
// would; ArchiveManager always archives to a new temporary name.
//
// # Downloads
//
// internal/downloader imports this package, so ArchiveManager takes the
// download manager through the DownloadManager interface, which
// `downloader.DownloadManager.Sync()` satisfies, and the loop through
// Loop (*http.Loop).
package archiver
