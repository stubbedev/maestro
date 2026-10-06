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
// the User-Agent maestro sends is documented on UserAgent. Process-wide PHP
// state the transport needs (php.ini for the CA search, the TLS library of
// PHP's libcurl) is installed with SetIniSource and SetCurlInfo.
//
// Where net/http is made to behave as curl: response header lines come
// from the bytes of the response heads (headcapture.go: wire order and
// spelling, 1xx heads and the proxy CONNECT head, the CONNECT tunnel
// being opened in tunnel.go), on HTTP/2 connections from their HEADERS
// frames, decoded by a second HPACK decoder kept in step with net/http's
// (h2capture.go), and a response's trailer follows its head as curl
// writes it; chunked bodies are followed as curl's parser reads them
// (chunks.go); content decoding covers gzip, deflate, br and zstd with
// the Accept-Encoding of the probed php's libcurl (encoding.go), gzip and
// deflate through a port of zlib's inflate for its error messages
// (inflate.go); client keys may be legacy PEM or PKCS#8 encrypted, scrypt
// included (clientkey.go); the peer name is checked as curl and PHP's
// stream wrapper check it, falling back on the common name (hostcheck.go);
// the stream wrapper's ssl ciphers (ciphers.go) and verify_depth apply to
// RemoteFilesystem; curl's error numbers and messages are libcurl 8.22's
// (curlError), the stream wrapper's warnings, its reading of 1xx heads
// and of bodies cut short PHP 8.4's (streamWarnings, headRecorder).
//
// Known differences from curl: no HTTP/3 (Composer asks libcurl for it
// when libcurl has it, and curl then races QUIC against TCP; when checked
// again, repo.packagist.org, packagist.org, api.github.com,
// codeload.github.com, getcomposer.org and gitlab.com answered php-curl
// over HTTP/2 and did not answer QUIC at all); a chunk-size line curl
// reads (junk after the hex digits, such as "2x" or "2 ;ext") fails in
// net/http's chunked reader; the C library's wording of a failed lookup
// in the stream wrapper's warning is glibc's (BSD's on macOS), not that
// of the platform's actual libc.
package http
