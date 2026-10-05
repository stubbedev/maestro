// Package composerrepo ports Composer\Repository\ComposerRepository
// (src/Composer/Repository/ComposerRepository.php): repositories served
// by a Composer repository server, Packagist included, in every protocol
// version Composer reads (v2 metadata-url with available-packages and
// patterns, v1 lazy providers, provider listings with includes, static
// packages.json with includes), with the security advisories and filter
// list APIs.
//
// Constructor is the "composer" type's repository.Constructor, which
// internal/composer registers through repository.Manager's ExternalTypes.
//
// # Requests and cache
//
// The repository makes the requests Composer makes, with the same
// transport options and If-Modified-Since headers, and keeps its
// metadata cache (cache-repo-dir) byte for byte as Composer does, so
// caches are interchangeable (tools/oracle/composerrepo checks both).
//
// # Concurrency
//
// Composer adds the requests for the metadata files of many packages at
// once and processes each response in a promise callback as it arrives.
// Here the requests are added in the same order; each response is
// decoded on its own goroutine as soon as it arrives. The rest of the
// callback (POST_FILE_DOWNLOAD, the repository's warnings, the cache
// write, the repository's state) runs on the calling goroutine in request
// order, so output and events are deterministic, and the packages of the
// files are then built in parallel and collected in request order. The
// cached copies of the files are read in order and decoded in parallel
// before the requests start. Event listeners therefore always run on the
// goroutine calling the repository.
//
// A ComposerRepository itself is not safe for concurrent use.
package composerrepo
