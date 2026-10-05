# Task: internal/pkg/archiver — Composer\Package\Archiver

Port .ref/composer/src/Composer/Package/Archiver: ArchiveManager (getPackageFilename/getPackageFilenameParts with its exact sanitising, archive() with the download-then-archive flow via internal/downloader's DownloadManager, the temp dir handling), ArchiverInterface, PharArchiver (tar/tar.gz/tar.bz2/zip writing — Composer uses PharData; produce archives whose extracted contents, names, order and modes match what PharData writes; document any byte-level differences in the archive container itself), ZipArchiver (ZipArchive), ArchivableFilesFinder, BaseExcludeFilter, ComposerExcludeFilter, GitExcludeFilter (.gitattributes export-ignore), and the Symfony Glob::toRegex port they need.

internal/downloader/archivablefiles.go currently holds a private copy of ArchivableFilesFinder, the exclude filters and Glob::toRegex (see HANDOFF.md): move that code here (this package becomes the owner), make internal/downloader's PathDownloader use it, and delete the copy — without changing downloader behaviour (its tests must keep passing).

Tests: port .ref/composer/tests/Composer/Test/Package/Archiver/* (ArchiveManagerTest, ArchivableFilesFinderTest, GitExcludeFilterTest, PharArchiverTest, ZipArchiverTest) with fixtures. Oracle: archive many generated package trees with real Composer's ArchiveManager (tools/oracle/archiver) and compare the extracted contents (paths, file contents, modes) and file lists.

Scope: internal/pkg/archiver, the move out of internal/downloader. Use internal/pkg, internal/util, internal/php, internal/downloader.
Report: public API, test counts, divergences, lint/test status.
