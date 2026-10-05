// Package vcs ports Composer\Repository\VcsRepository and the VCS drivers
// of Composer\Repository\Vcs: GitHub, GitLab, Bitbucket and Forgejo
// through their APIs (falling back to git), and git, hg, svn, fossil and
// perforce through their command-line tools (internal/util/vcs).
//
// # Class hierarchy
//
// VcsRepository extends ArrayRepository through repository.Extend, so its
// packages are loaded by the first call that needs them, as in PHP.
// VcsDriverInterface is Driver; the abstract VcsDriver is the unexported
// vcsDriver every driver embeds, which dispatches the methods PHP calls
// through $this (getFileContent, getChangeDate, getComposerInformation,
// shouldCache) to the embedding driver. A driver class is a DriverType:
// its constructor and its static supports(). VcsRepository tries
// DefaultDrivers in Composer's order unless the repository type names
// one.
//
// # Processes
//
// The static supports() checks run their probe (`git tag`, `hg summary`,
// `svn info`, ...) through the repository's process executor where
// Composer creates a new ProcessExecutor; the commands are the same.
//
// # Errors
//
// PHP exceptions are the util error types (util.RuntimeError,
// util.InvalidArgumentError, util.LogicError, util.TransportError,
// repository.InvalidRepositoryError, ...); `catch (\RuntimeException)` is
// util.IsRuntimeException. As in the other ports, a PCRE failure of a
// pattern match (only possible on backtracking limits) reads as no match.
package vcs
