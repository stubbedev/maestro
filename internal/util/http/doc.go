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
// spelling, 1xx and proxy CONNECT heads included, the CONNECT tunnel being
// opened in tunnel.go); content decoding covers gzip, deflate, br and zstd
// with the Accept-Encoding of the probed php's libcurl (encoding.go);
// client keys may be legacy PEM or PKCS#8 encrypted (clientkey.go); the
// stream wrapper's ssl ciphers (ciphers.go) and verify_depth apply to
// RemoteFilesystem; curl's error numbers and messages are libcurl 8.22's
// (curlError), the stream wrapper's warnings PHP's (streamWarnings).
//
// Known differences from curl: HTTP/2 response fields are rebuilt from
// net/http's map (lowercase names, sorted, not in wire order: net/http's
// HTTP/2 client exposes neither the HPACK field order nor its connection);
// no HTTP/3 (Composer asks libcurl for it when libcurl has it, and curl
// then races QUIC against TCP; Packagist, api.github.com and
// codeload.github.com answered php-curl over HTTP/2 when checked); PKCS#8
// keys encrypted with scrypt are not supported (they need
// golang.org/x/crypto); certificates without a subjectAltName are not
// matched on their CN; some curl messages keep Go's detail where curl
// names more (a bad chunk length, zlib's reason for corrupt deflate data).
// The stream wrapper takes a 103 Early Hints head for the response, which
// net/http skips, and words DNS failures and timeouts as PHP's network
// layer does, which RemoteFilesystem reports in curl's words.
package http
