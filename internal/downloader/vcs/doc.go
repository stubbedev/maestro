// Package vcs ports Composer's VCS downloaders (src/Composer/Downloader):
// VcsDownloader and its Git, Hg, Svn, Fossil and Perforce subclasses, the
// downloaders of "source" installs. Register adds them to a
// downloader.DownloadManager under their types, as
// Factory::createDownloadManager does.
//
// The downloaders run their commands synchronously, as Composer's do: the
// promises they return are settled, except Remove's, which waits for the
// asynchronous directory removal. GitDownloader clones through Composer's
// bare mirror cache (cache-vcs-dir, Composer\Util\Git::syncMirror) when git
// is 2.3 or newer, and asks before discarding, stashing or showing local
// changes of an updated or removed package in interactive mode (the
// discard-changes setting decides otherwise).
//
// Downloader state (stashed and discarded changes, mirrored references) is
// per instance and guarded by a mutex; nothing is package-level.
package vcs
