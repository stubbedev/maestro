// Package http ports Composer's HTTP layer: Composer\Util\Http (CurlDownloader,
// Response, ProxyManager, ProxyItem, RequestProxy) and the Composer\Util
// classes bound up with it (HttpDownloader, RemoteFilesystem,
// StreamContextFactory, AuthHelper, GitHub, GitLab, Bitbucket, Forgejo,
// Loop, SyncHelper), plus composer/ca-bundle's CaBundle and
// Factory::createHttpDownloader. They form one package because Composer's
// classes reference each other in a cycle (CurlDownloader → AuthHelper →
// GitHub → HttpDownloader → CurlDownloader).
//
// curl is replaced by net/http (transport.go). The behaviour Composer
// relies on is kept: its retries, redirect handling, authentication
// prompts, error messages (curl's error numbers and texts included),
// debug output and concurrency limit. Transfers run on goroutines; their
// results are processed, and promises settled (running their callbacks),
// on the goroutine waiting in HttpDownloader or Loop, as PHP processes
// them inside wait(), in the order the transfers started (util.Scheduler).
//
// Composer's free-form option arrays ('http' => [...], 'ssl' => [...],
// 'retry-auth-failure', 'max_file_size', ...) are *php.Array values with
// the same keys. A *php.Array cannot hold a callable, so the
// prevent_url_access_callable and prevent_ip_access_callable options hold
// a handle from RegisterCallable.
//
// Facts Composer reads from statics living above this package (the running
// command, the platform PHP version) come through the Runtime interface;
// the User-Agent maestro sends is documented on UserAgent.
package http
