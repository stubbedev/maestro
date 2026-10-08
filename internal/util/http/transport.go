// The net/http transfer engine behind CurlDownloader and RemoteFilesystem:
// what curl (and PHP's http stream wrapper) do for Composer, built on
// net/http. Errors are reported as curl reports them (errno and message),
// because Composer's messages, retries and hints are keyed on those.

package http

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/http2/hpack"

	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/util"
)

// curl error numbers Composer distinguishes.
const (
	curleUnsupportedProtocol = 1
	curleCouldntResolveHost  = util.CurleCouldntResolveHost
	curleCouldntConnect      = 7
	curleHTTP2               = 16
	curlePartialFile         = 18
	curleWriteError          = 23
	curleOperationTimedout   = util.CurleOperationTimedout
	curleSSLConnectError     = 35
	curleGotNothing          = 52
	curleSendError           = 55
	curleRecvError           = 56
	curleSSLCertproblem      = 58
	curlePeerFailedVerify    = 60
	curleBadContentEncoding  = 61
	curleSSLCacertBadfile    = 77
	curleHTTP2Stream         = 92
)

// tlsSettings are the ssl options a transfer uses.
type tlsSettings struct {
	cafile, capath     string
	verifyPeer         bool
	verifyPeerName     bool
	localCert, localPK string
	passphrase         string
	allowSelfSigned    bool
	// ciphers is the OpenSSL cipher list and verifyDepth the maximum
	// depth of the peer's chain (PHP's stream wrapper only).
	ciphers        string
	hasCiphers     bool
	verifyDepth    int64
	hasVerifyDepth bool
	// stream checks the peer name as PHP's stream wrapper does, not as
	// curl does (checkPeerName).
	stream bool
}

// transportKey identifies a pooled transport: requests sharing one share
// its connections (curl's connection cache).
type transportKey struct {
	tls       tlsSettings
	proxy     string
	proxyAuth string
	// proxyHeader is a Proxy-Authorization value for CONNECT requests.
	proxyHeader string
	ipResolve   int
	fresh       bool
	http1       bool
	// lane tells apart the transports of the transfers started ahead
	// beyond one connection's streams (prefetchLanes): each lane opens
	// connections of its own.
	lane int
}

// transferRequest describes one HTTP exchange.
type transferRequest struct {
	url     string
	method  string
	content *string
	headers []string
	// timeout bounds the whole transfer (CURLOPT_TIMEOUT); readTimeout,
	// for the stream wrapper, only the wait for the response.
	timeout, readTimeout time.Duration
	connectTimeout       time.Duration
	key                  transportKey
	// decode makes the transport undo content encodings as curl does for
	// CURLOPT_ENCODING "" (encoding.go), advertising them unless a header
	// does.
	decode bool
	// http1Status reports HTTP/2 status lines as curl does ("HTTP/2 200 ").
	curlStatusLines bool
	// body receives the (decoded) body; nil keeps it in memory.
	body *os.File
	// limit stops reading the body after that many bytes (file_get_contents'
	// maxlen); 0 reads it all.
	limit int64
	// maxFileSize aborts transfers announcing or receiving more bytes; 0
	// is unlimited.
	maxFileSize int64
	// preventIP, when set, blocks connections to the IPs it rejects.
	preventIP func(ip string) bool
	// safeURL is the URL as messages show it.
	safeURL string
}

// transferResult is what a finished transfer reports.
type transferResult struct {
	// fail is set (Errno != 0) when the transfer failed like curl fails.
	fail util.CurlFailure
	// err, when set, rejects the job as it is (max size, blocked IP).
	err error
	// streamWarnings, when set, are the warnings PHP's http stream wrapper
	// raises for the failure, which RemoteFilesystem reports instead of
	// errMsg.
	streamWarnings []string

	status  int
	headers []string
	body    []byte
	info    util.TransferInfo
}

// transportPool builds and caches transports (processTransports, and a
// RemoteFilesystem's own).
type transportPool struct {
	mu         sync.Mutex
	transports map[transportKey]*pooledTransport
	// pre are the connections opened ahead (Preconnect).
	pre map[preKey]*preconn
	// first are the first connection attempts (firstconn.go).
	first map[firstConnKey]*firstConn
}

// pooledTransport is a transport of the pool with the TLS configuration
// its connections are opened with.
type pooledTransport struct {
	client *http.Client
	// tls is a snapshot of the transport's TLS configuration taken once
	// net/http set it up (HTTP/2's NextProtos added), before the
	// transport is handed out. It is never written to: connections are
	// opened from clones of it, and the transport's own TLSClientConfig,
	// which net/http owns and may write to, is not read once it is in use.
	tls *tls.Config
}

func (p *transportPool) client(key transportKey, connectTimeout time.Duration) (*http.Client, *transferResult) {
	pt, failure := p.transport(key, connectTimeout)
	if failure != nil {
		return nil, failure
	}

	return pt.client, nil
}

// transport returns the pooled transport for the key, built on first use.
// It is fully set up, net/http's HTTP/2 configuration included, before
// it is stored, so goroutines sharing it (transfers, Preconnect) only
// read what the build wrote.
func (p *transportPool) transport(key transportKey, connectTimeout time.Duration) (*pooledTransport, *transferResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pt, ok := p.transports[key]; ok {
		return pt, nil
	}

	tlsConfig, failure := buildTLSConfig(key.tls)
	if failure != nil {
		return nil, failure
	}

	network := dialNetwork(key)
	dialer := newDialer(connectTimeout)

	t := &http.Transport{
		TLSClientConfig:     tlsConfig,
		TLSHandshakeTimeout: connectTimeout,
		ForceAttemptHTTP2:   !key.http1,
		DisableCompression:  true,
		DisableKeepAlives:   key.fresh,
		MaxConnsPerHost:     8,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     118 * time.Second,
	}

	if key.http1 {
		t.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}

	var proxyURL *url.URL

	if key.proxy != "" {
		if u, err := url.Parse(key.proxy); err == nil {
			if key.proxyAuth != "" {
				user, pass, _ := strings.Cut(key.proxyAuth, ":")
				u.User = url.UserPassword(user, pass)
			}

			proxyURL = u
		}
	}

	if proxyURL != nil {
		// http requests go to the proxy; https ones through a CONNECT
		// tunnel DialTLSContext opens (tunnel.go)
		t.Proxy = func(req *http.Request) (*url.URL, error) {
			if req.URL.Scheme == "https" {
				return nil, nil
			}

			return proxyURL, nil
		}
	}

	t.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		c, err := dialer.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}

		return &headConn{Conn: c}, nil
	}

	pt := &pooledTransport{}

	t.DialTLSContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
		var (
			conn    *tls.Conn
			connect []byte
			err     error
		)

		switch {
		case proxyURL == nil:
			// net/http's own TLS dial (dialTLS), unless a connection was
			// opened ahead (Preconnect)
			if c := p.takePreconnected(ctx, key, addr); c != nil {
				conn, _ = c.(*tls.Conn)
			}

			if conn == nil {
				conn, err = dialTLS(ctx, dialer, network, addr, pt.tls, t.TLSHandshakeTimeout)
			}
		case proxyURL.Scheme == "https" && addr == canonicalProxyAddr(proxyURL):
			// the TLS connection to an https proxy, for http requests;
			// curl speaks HTTP/1.1 to proxies (CURLPROXY_HTTPS)
			cfg := pt.tls.Clone()
			cfg.NextProtos = nil
			conn, err = dialTLS(ctx, dialer, network, addr, cfg, t.TLSHandshakeTimeout)
		default:
			conn, connect, err = dialTunnel(ctx, dialer, network, proxyURL, key.proxyHeader, addr, pt.tls, t.TLSHandshakeTimeout)
		}

		if err != nil {
			return nil, err
		}

		return recordingTLSConn(conn, connect), nil
	}

	// net/http sets HTTP/2 up (adding "h2" and "http/1.1" to the
	// NextProtos of TLSClientConfig) on the transport's first use, which
	// CloseIdleConnections is; done here, before the transport is shared,
	// that write cannot race with goroutines opening connections for it
	t.CloseIdleConnections()
	pt.tls = t.TLSClientConfig.Clone()

	pt.client = &http.Client{
		Transport: t,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if p.transports == nil {
		p.transports = map[transportKey]*pooledTransport{}
	}

	p.transports[key] = pt

	return pt, nil
}

// buildTLSConfig turns ssl options into a tls.Config, failing like curl
// does for unusable CA or client certificate files.
func buildTLSConfig(s tlsSettings) (*tls.Config, *transferResult) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if s.cafile != "" || s.capath != "" {
		pool, err := loadCertPool(s.cafile, s.capath)
		if err != nil {
			return nil, &transferResult{fail: util.CurlFailure{Errno: curleSSLCacertBadfile, Message: "error setting certificate verify locations:  CAfile: " + orNone(s.cafile) + " CApath: " + orNone(s.capath)}}
		}

		cfg.RootCAs = pool
	}

	if s.localCert != "" {
		cert, failure := loadClientCertificate(s.localCert, s.localPK, s.passphrase)
		if failure != nil {
			return nil, &transferResult{fail: util.CurlFailure{Errno: failure.errno, Message: failure.msg}}
		}

		cfg.Certificates = []tls.Certificate{cert}
	}

	if s.hasCiphers {
		suites, anyCipher := evalCipherList(s.ciphers)
		if !anyCipher {
			// SSL_CTX_set_cipher_list fails: no crypto for the stream
			return nil, &transferResult{fail: util.CurlFailure{Errno: curleSSLConnectError, Message: "operation failed"}, streamWarnings: []string{"Failed to enable crypto", "Failed to open stream: operation failed"}}
		}

		if len(suites) == 0 {
			// only suites crypto/tls lacks (DHE, CAMELLIA, ...): TLS 1.3,
			// whose suites the list does not restrict, is what remains
			cfg.MinVersion = tls.VersionTLS13
		}

		cfg.CipherSuites = suites
	}

	maxDepth := int64(-1)
	if s.hasVerifyDepth {
		maxDepth = s.verifyDepth
	}

	if !s.verifyPeer {
		cfg.InsecureSkipVerify = true // verify_peer false asks for exactly this

		return cfg, nil
	}

	// The peer is verified in VerifyConnection: the chain as OpenSSL
	// does, accepting a self-signed leaf for allow_self_signed, then the
	// peer name as curl or PHP's stream wrapper check it (both fall back
	// on the common name, crypto/tls does not), unless verify_peer_name
	// is off.
	cfg.InsecureSkipVerify = true // verification is done in VerifyConnection
	roots := cfg.RootCAs
	checkName := s.verifyPeerName
	selfSigned := s.allowSelfSigned
	stream := s.stream
	cfg.VerifyConnection = func(cs tls.ConnectionState) error {
		return verifyPeer(cs, roots, checkName, selfSigned, maxDepth, stream)
	}

	return cfg, nil
}

// errChainTooLong is X509_V_ERR_CERT_CHAIN_TOO_LONG.
var errChainTooLong = errors.New("certificate chain too long")

// checkChainDepth is the verify_depth check of PHP's verify callback
// (php_openssl_verify_callback): a certificate deeper in the chain than
// the limit fails it, the leaf being at depth 0 and the trust anchor the
// deepest. The shortest chain counts, as OpenSSL stops at the first
// trusted certificate.
func checkChainDepth(chains [][]*x509.Certificate, maxDepth int64) error {
	if maxDepth < 0 || len(chains) == 0 {
		return nil
	}

	shortest := len(chains[0])
	for _, c := range chains[1:] {
		shortest = min(shortest, len(c))
	}

	if int64(shortest-1) > maxDepth {
		return errChainTooLong
	}

	return nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}

	return s
}

// verifyPeer is the peer verification of a connection (VerifyConnection):
// the chain, then the peer name when checkName is set.
func verifyPeer(cs tls.ConnectionState, roots *x509.CertPool, checkName, allowSelfSigned bool, maxDepth int64, stream bool) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("tls: no peer certificates")
	}

	leaf := cs.PeerCertificates[0]
	opts := x509.VerifyOptions{Roots: roots, Intermediates: x509.NewCertPool()}

	for _, c := range cs.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}

	chains, err := leaf.Verify(opts)

	switch {
	case err == nil:
		err = checkChainDepth(chains, maxDepth)
	case allowSelfSigned && leaf.CheckSignatureFrom(leaf) == nil:
		err = nil
	default:
		// as crypto/tls reports it
		err = &tls.CertificateVerificationError{UnverifiedCertificates: cs.PeerCertificates, Err: err}
	}

	if err != nil || !checkName {
		return err
	}

	return checkPeerName(leaf, cs.ServerName, stream)
}

// certPools caches parsed CA files and directories per process.
var certPools sync.Map // string -> *x509.CertPool

func loadCertPool(cafile, capath string) (*x509.CertPool, error) {
	key := cafile + "\x00" + capath
	if p, ok := certPools.Load(key); ok {
		pool, _ := p.(*x509.CertPool)

		return pool, nil
	}

	pool := x509.NewCertPool()

	if cafile != "" {
		data, err := os.ReadFile(cafile)
		if err != nil || !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("bad CA file")
		}
	}

	if capath != "" {
		entries, err := os.ReadDir(capath)
		if err != nil {
			return nil, err
		}

		for _, e := range entries {
			if data, err := os.ReadFile(filepath.Join(capath, e.Name())); err == nil {
				pool.AppendCertsFromPEM(data)
			}
		}
	}

	certPools.Store(key, pool)

	return pool, nil
}

// do runs a transfer to completion. ctx cancels it (abortRequest).
func (p *transportPool) do(ctx context.Context, r *transferRequest) *transferResult {
	start := time.Now()
	res := &transferResult{info: util.TransferInfo{URL: r.url, DownloadContentLength: -1}}

	finish := func() *transferResult {
		res.info.TotalTime = time.Since(start).Seconds()
		if res.fail.Errno != 0 {
			res.info.ErrorCode = res.fail.Errno
		}

		return res
	}

	client, failure := p.client(r.key, r.connectTimeout)
	if failure != nil {
		failure.info = res.info

		return failure
	}

	if r.timeout > 0 {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, r.timeout)
		defer cancel()
	}

	ctx, cancelTransfer := context.WithCancelCause(ctx)
	defer cancelTransfer(nil)

	// The stream wrapper's timeout is an idle timeout: it restarts with
	// every read. It runs from the connection on (GotConn): the connect
	// and the TLS handshake have their own (connectTimeout).
	var idle *time.Timer
	if r.readTimeout > 0 {
		idle = time.AfterFunc(time.Hour, func() { cancelTransfer(context.DeadlineExceeded) })
		idle.Stop()

		defer idle.Stop()
	}

	var (
		bodyReader io.Reader
		method     = r.method
	)

	if r.content != nil {
		bodyReader = strings.NewReader(*r.content)
		if method == "" {
			method = http.MethodPost
		}
	}

	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, r.url, bodyReader)
	if err != nil {
		res.fail = util.CurlFailure{Errno: curleUnsupportedProtocol, Message: err.Error()}

		return finish()
	}

	var (
		connected atomic.Bool
		primaryIP atomic.Pointer[string]
		// recorder records the response heads (HTTP/1), h2 the stream
		// (HTTP/2) and connectHead the CONNECT head of an HTTP/2
		// connection's tunnel.
		recorder    atomic.Pointer[headRecorder]
		h2          h2Capture
		connectHead atomic.Pointer[[]byte]
	)

	defer h2.release()

	host := req.URL.Hostname()
	port := req.URL.Port()

	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}

	// the peer curl names in connect errors: the target, or for a request
	// sent to a proxy (http through a proxy) the proxy, followed by
	// " over proxy <proxy host>"
	peerHost, peerPort, via := host, port, ""

	if r.key.proxy != "" {
		if pu, err := url.Parse(r.key.proxy); err == nil {
			via = " over proxy " + pu.Hostname()

			if req.URL.Scheme != "https" {
				peerHost, peerPort, _ = net.SplitHostPort(canonicalProxyAddr(pu))
			}
		}
	}

	// the first transfer to the address, which the others wait for
	// (awaitFirstConn)
	firstConnected, firstSent, firstDone := p.awaitFirstConn(ctx, r.key, req.URL)
	defer firstDone()

	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connected.Store(true)

			if hc, ok := info.Conn.(*h2HeadConn); ok {
				firstConnected(hc)
			} else {
				firstConnected(nil)
			}

			if idle != nil {
				idle.Reset(r.readTimeout)
			}

			if hc := asHeadConn(info.Conn); hc != nil {
				recorder.Store(hc.record(!r.curlStatusLines, method == http.MethodHead))
			} else if hc, ok := info.Conn.(*h2HeadConn); ok {
				h2.gotConn(hc)

				if head := hc.takeConnect(); head != nil {
					connectHead.Store(&head)
				}
			}

			if addr, ok := info.Conn.RemoteAddr().(*net.TCPAddr); ok {
				ip := addr.IP.String()
				primaryIP.Store(&ip)

				if r.preventIP != nil && r.preventIP(ip) {
					cancelTransfer(util.NewTransportError(`IP "`+ip+`" is blocked for "`+r.safeURL+`".`, 400))
				}
			}
		},
		WroteHeaders: func() {
			h2.wroteHeaders()
			firstSent()
		},
		ConnectStart: func(string, string) { firstSent() },
	}
	req = req.WithContext(httptrace.WithClientTrace(ctx, trace))

	setRequestHeaders(req, r)

	resp, err := client.Do(req)

	if ip := primaryIP.Load(); ip != nil {
		res.info.PrimaryIP = *ip
	}

	if err != nil {
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
			res.err = cause

			return finish()
		}

		res.fail = curlError(ctx, err, peerHost, peerPort, via, connected.Load(), time.Since(start), 0, -1)
		if !r.curlStatusLines {
			res.streamWarnings = streamWarnings(err, connected.Load())
		}

		return finish()
	}
	defer resp.Body.Close()

	res.status = resp.StatusCode
	res.info.HTTPCode = resp.StatusCode
	res.info.DownloadContentLength = resp.ContentLength
	h2Heads, h2OK := h2.heads()
	headText := ""
	res.headers, headText = headerLines(resp, r.curlStatusLines, recorder.Load(), connectHead.Load(), h2Heads, h2OK)

	if r.maxFileSize > 0 && resp.ContentLength > r.maxFileSize {
		res.err = util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Content-length header indicates " + strconv.FormatInt(resp.ContentLength, 10) + " bytes. Allowed " + strconv.FormatInt(r.maxFileSize, 10) + " bytes for " + r.safeURL)

		return finish()
	}

	counter := &countingReader{r: resp.Body, max: r.maxFileSize, idle: idle, idleTimeout: r.readTimeout}

	var src io.Reader = counter

	if r.decode {
		decoded := decodingReader(src, strings.Join(resp.Header.Values("Content-Encoding"), ","))
		if c, ok := decoded.(io.Closer); ok {
			defer c.Close()
		}

		src = decoded
	}

	if r.limit > 0 {
		src = io.LimitReader(src, r.limit)
	}

	if r.body != nil {
		_, err = io.Copy(fileWriter{r.body}, src)
	} else {
		var buf bytes.Buffer
		if resp.ContentLength > 0 && resp.ContentLength < 64<<20 {
			buf.Grow(int(resp.ContentLength))
		}

		_, err = buf.ReadFrom(src)
		res.body = buf.Bytes()
	}

	res.info.SizeDownload = counter.n

	if err == nil {
		applyBodyEnd(res, r, recorder.Load(), &h2, headText)
	}

	// curl reads a chunked body itself, failing where net/http may not,
	// in its own words (chunks.go)
	chunkFailure := ""
	if rec := recorder.Load(); r.curlStatusLines && rec != nil {
		chunkFailure = rec.chunkFailure()
	}

	if err == nil && chunkFailure != "" {
		res.fail = util.CurlFailure{Errno: curleRecvError, Message: chunkFailure}
	}

	if err != nil {
		var (
			wErr   *writeError
			encErr *encodingError
		)

		switch {
		case errors.As(err, &encErr):
			res.fail = util.CurlFailure{Errno: encErr.errno, Message: encErr.msg}
		case errors.Is(err, errMaxSize):
			res.err = util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Downloaded " + strconv.FormatInt(counter.n, 10) + " of allowed " + strconv.FormatInt(r.maxFileSize, 10) + " bytes for " + r.safeURL)
		case errors.As(err, &wErr):
			res.fail = util.CurlFailure{Errno: curleWriteError, Message: "Failure writing output to destination"}
		default:
			if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
				res.err = cause
			} else if !r.curlStatusLines {
				// PHP's stream wrapper returns what it read before the
				// connection failed, closed or timed out, without a
				// warning (RemoteFilesystem then checks Content-Length)
			} else if chunkFailure != "" {
				res.fail = util.CurlFailure{Errno: curleRecvError, Message: chunkFailure}
			} else {
				res.fail = curlError(ctx, err, peerHost, peerPort, via, true, time.Since(start), counter.n, resp.ContentLength)
				if res.fail.Errno == curlePartialFile && resp.ContentLength > 0 {
					// lib/transfer.c (8.x)
					res.fail.Message = "end of response with " + strconv.FormatInt(resp.ContentLength-counter.n, 10) + " bytes missing"
				}
			}
		}
	}

	return finish()
}

// setRequestHeaders applies Composer's header lines the way curl applies
// CURLOPT_HTTPHEADER: "Name: value" adds or replaces, "Name:" removes an
// internal header, "Name;" sends it empty.
func setRequestHeaders(req *http.Request, r *transferRequest) {
	h := req.Header
	userAgent, accept, acceptEncodingSet := false, false, false

	for _, line := range r.headers {
		name, value, found := strings.Cut(line, ":")
		if !found {
			if n, ok := strings.CutSuffix(line, ";"); ok && n != "" {
				h[n] = append(h[n], "")
			}

			continue
		}

		name = strings.TrimSpace(name)
		value = strings.Trim(value, " \t")

		if name == "" {
			continue
		}

		switch php.Strtolower(name) {
		case "host":
			req.Host = value

			continue
		case "connection":
			// net/http manages the connection; Composer always asks for
			// keep-alive (curl) or close (streams).
			if php.Strcasecmp(value, "close") == 0 {
				req.Close = true
			}

			continue
		case "content-length", "transfer-encoding":
			continue
		case "user-agent":
			userAgent = true
		case "accept":
			accept = true
		case "accept-encoding":
			acceptEncodingSet = true
		}

		if value == "" {
			delete(h, name)

			continue
		}

		h[name] = append(h[name], value)
	}

	if !accept {
		h["Accept"] = []string{"*/*"}
	}

	if !userAgent {
		// curl sends none without CURLOPT_USERAGENT; Go would send its own.
		h["User-Agent"] = []string{""}
	}

	if r.decode && !acceptEncodingSet {
		if v := curlAcceptEncoding(curlInfo()); v != "" {
			h["Accept-Encoding"] = []string{v}
		}
	}

	if r.content != nil && h.Get("Content-Type") == "" {
		h["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	}
}

// headerLines are the header lines of a response: for curl (curlStatus)
// Composer's explode("\r\n", rtrim(...)) of what curl wrote to its header
// handle, CONNECT and 1xx heads included; for the stream wrapper
// $http_response_header. They come from the heads recorded on the
// connection (headcapture.go; h2Heads for HTTP/2, h2capture.go), else
// from net/http's parse of the final one. text is the header text curl
// wrote when it is known from the wire ("" for the stream wrapper).
func headerLines(resp *http.Response, curlStatus bool, rec *headRecorder, h2Connect *[]byte, h2Heads [][]hpack.HeaderField, h2OK bool) (lines []string, text string) {
	var (
		connect, heads []byte
		ok             bool
	)

	if rec != nil {
		connect, heads, ok = rec.heads()
	} else if h2Connect != nil {
		connect = *h2Connect
	}

	switch {
	case !curlStatus && ok:
		return streamHeaderLines(heads), ""
	case !curlStatus:
		return responseHeaderLines(resp, false), ""
	case ok:
		text = curlHeaderText(append(append([]byte(nil), connect...), heads...))
	case h2OK:
		text = curlHeaderText(connect) + curlH2HeaderText(h2Heads)
	default:
		return curlHeaderLines(curlHeaderText(connect) + strings.Join(responseHeaderLines(resp, true), "\r\n")), ""
	}

	return curlHeaderLines(text), text
}

// applyBodyEnd completes a result once its body was read through: for
// curl the header lines get the trailer curl writes to the header handle
// after the body (HTTP/1 chunked or HTTP/2); for the stream wrapper an
// informational head it took for the response (headRecorder.feed) gives
// the status and the body.
func applyBodyEnd(res *transferResult, r *transferRequest, rec *headRecorder, h2 *h2Capture, headText string) {
	if r.curlStatusLines {
		trailer := ""

		switch {
		case headText == "":
		case rec != nil:
			trailer = string(rec.trailer())
		default:
			trailer = curlH2TrailerText(h2.trailer())
		}

		if trailer != "" {
			res.headers = curlHeaderLines(headText + trailer)
		}

		return
	}

	if rec == nil || r.body != nil {
		return
	}

	if code, body, ok := rec.earlyResponse(); ok {
		if r.limit > 0 && int64(len(body)) > r.limit {
			body = body[:r.limit]
		}

		res.status, res.info.HTTPCode = code, code
		res.body = body
	}
}

// responseHeaderLines renders a response head net/http parsed as header
// lines: the status line, then the fields. It is the fallback when the
// head was not recorded (an HTTP/2 header block the capture could not
// decode, an HTTP/1 head above maxRecordedHead): net/http does not keep
// the order fields arrived in, so they are sorted by name; HTTP/2 names
// are lowercase as on the wire, and curl writes the status line as
// "HTTP/2 200 ".
func responseHeaderLines(resp *http.Response, curlStatus bool) []string {
	lines := make([]string, 0, len(resp.Header)+1)

	switch {
	case resp.ProtoMajor == 2 && curlStatus:
		lines = append(lines, "HTTP/2 "+strconv.Itoa(resp.StatusCode)+" ")
	case resp.ProtoMajor == 2:
		lines = append(lines, "HTTP/2.0 "+resp.Status)
	default:
		lines = append(lines, resp.Proto+" "+resp.Status)
	}

	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		shown := name
		if resp.ProtoMajor == 2 {
			shown = php.Strtolower(name)
		}

		for _, v := range resp.Header[name] {
			lines = append(lines, shown+": "+v)
		}
	}

	return lines
}

var errMaxSize = errors.New("maximum file size exceeded")

// countingReader counts the bytes received on the wire and enforces the
// maximum size.
type countingReader struct {
	r           io.Reader
	n           int64
	max         int64
	idle        *time.Timer
	idleTimeout time.Duration
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)

	if c.idle != nil && n > 0 {
		c.idle.Reset(c.idleTimeout)
	}

	if c.max > 0 && c.n > c.max {
		return n, errMaxSize
	}

	return n, err
}

// writeError marks failures writing the body to its destination.
type writeError struct{ err error }

func (e *writeError) Error() string { return e.err.Error() }

// fileWriter wraps the body file so write failures are told apart from
// network ones.
type fileWriter struct{ f *os.File }

func (w fileWriter) Write(b []byte) (int, error) {
	n, err := w.f.Write(b)
	if err != nil {
		return n, &writeError{err}
	}

	return n, nil
}

// curlError maps a net/http failure to the curl error number and message
// libcurl 8.22 (the one PHP links in the reference environment, with
// OpenSSL 3) reports for the same failure; the messages were compared on
// the same failures with php-curl. host and port name the peer, via the
// proxy (" over proxy <host>", or ""); received is the body bytes read so
// far and total the Content-Length (-1 when unknown).
func curlError(ctx context.Context, err error, host, port, via string, connected bool, elapsed time.Duration, received, total int64) util.CurlFailure {
	ms := strconv.FormatInt(elapsed.Milliseconds(), 10)

	if te, ok := errors.AsType[*tunnelError](err); ok {
		return util.CurlFailure{Errno: te.errno, Message: te.msg}
	}

	if _, ok := errors.AsType[tlsHandshakeTimeoutError](err); ok || errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		if !connected {
			return util.CurlFailure{Errno: curleOperationTimedout, Message: "Connection timed out after " + ms + " milliseconds", Timeout: util.TimeoutConnecting}
		}

		if total >= 0 {
			return util.CurlFailure{Errno: curleOperationTimedout, Message: "Operation timed out after " + ms + " milliseconds with " + strconv.FormatInt(received, 10) + " out of " + strconv.FormatInt(total, 10) + " bytes received", Timeout: util.TimeoutTransfer}
		}

		return util.CurlFailure{Errno: curleOperationTimedout, Message: "Operation timed out after " + ms + " milliseconds with " + strconv.FormatInt(received, 10) + " bytes received", Timeout: util.TimeoutTransfer}
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		if dnsErr.IsTimeout {
			return util.CurlFailure{Errno: curleOperationTimedout, Message: "Resolving timed out after " + ms + " milliseconds", Timeout: util.TimeoutResolving}
		}

		return util.CurlFailure{Errno: curleCouldntResolveHost, Message: "Could not resolve host: " + host}
	}

	if f, ok := certificateError(err); ok {
		return f
	}

	if opErr := dialError(err); opErr != nil {
		if opErr.Timeout() {
			return util.CurlFailure{Errno: curleOperationTimedout, Message: "Connection timed out after " + ms + " milliseconds", Timeout: util.TimeoutConnecting}
		}

		return util.CurlFailure{Errno: curleCouldntConnect, Message: "Failed to connect to " + host + ":" + port + via + " after " + ms + " ms: Could not connect to server"}
	}

	// the TLS handshake (Curl_ossl_connect): curl reports OpenSSL's error
	// queue as "TLS connect error: <error>", or the socket error
	if _, ok := errors.AsType[*tlsHandshakeError](err); ok {
		switch {
		case isConnReset(err):
			return util.CurlFailure{Errno: curleSSLConnectError, Message: "Recv failure: Connection reset by peer", Reset: true}
		case errors.Is(err, syscall.EPIPE):
			return util.CurlFailure{Errno: curleSSLConnectError, Message: "Send failure: Broken pipe"}
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return util.CurlFailure{Errno: curleSSLConnectError, Message: "TLS connect error: error:0A000126:SSL routines::unexpected eof while reading"}
		}

		if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
			return util.CurlFailure{Errno: curleSSLConnectError, Message: "TLS connect error: error:0A00010B:SSL routines::wrong version number"}
		}

		if code, reason, ok := opensslAlert(err); ok {
			return util.CurlFailure{Errno: curleSSLConnectError, Message: "TLS connect error: error:" + code + ":SSL routines::" + reason}
		}

		return util.CurlFailure{Errno: curleSSLConnectError, Message: "TLS connect error: " + err.Error()}
	}

	if errors.Is(err, http.ErrSchemeMismatch) {
		return util.CurlFailure{Errno: curleSSLConnectError, Message: "TLS connect error: error:0A00010B:SSL routines::wrong version number"}
	}

	// an alert after the handshake, read with the response (a TLS 1.3
	// server refusing the client certificate)
	if code, reason, ok := opensslAlert(err); ok {
		return util.CurlFailure{Errno: curleRecvError, Message: "OpenSSL SSL_read: " + curlInfo().SSLVersion + ": error:" + code + ":SSL routines::" + reason + ", errno 0"}
	}

	if isConnReset(err) {
		return util.CurlFailure{Errno: curleRecvError, Message: "Recv failure: Connection reset by peer", Reset: true}
	}

	if errors.Is(err, syscall.EPIPE) {
		return util.CurlFailure{Errno: curleSendError, Message: "Send failure: Broken pipe"}
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "stream error"):
		return util.CurlFailure{Errno: curleHTTP2Stream, Message: "HTTP/2 stream was not closed cleanly: " + msg}
	case strings.Contains(msg, "http2:"):
		return util.CurlFailure{Errno: curleHTTP2, Message: "Error in the HTTP2 framing layer"}
	case strings.Contains(msg, "malformed HTTP response") && !strings.Contains(msg, `response "HTTP/`):
		// a status line not starting with "HTTP/" is HTTP/0.9 to curl
		return util.CurlFailure{Errno: curleUnsupportedProtocol, Message: "Received HTTP/0.9 when not allowed"}
	case errors.Is(err, io.ErrUnexpectedEOF):
		return util.CurlFailure{Errno: curlePartialFile, Message: "transfer closed with outstanding read data remaining"}
	case errors.Is(err, io.EOF), isServerClosedIdle(err):
		if connected {
			return util.CurlFailure{Errno: curleGotNothing, Message: "Empty reply from server"}
		}

		return util.CurlFailure{Errno: curleCouldntConnect, Message: "Failed to connect to " + host + ":" + port + via + " after " + ms + " ms: Could not connect to server"}
	}

	return util.CurlFailure{Errno: curleRecvError, Message: "Failure when receiving data from the peer: " + msg}
}

// certificateError is curl's report of a peer certificate OpenSSL (or
// curl's host name check) rejects: "SSL certificate OpenSSL verify
// result: <X509_verify_cert_error_string> (<code>)".
func certificateError(err error) (util.CurlFailure, bool) {
	var (
		unknownAuthority x509.UnknownAuthorityError
		invalidCert      x509.CertificateInvalidError
		verifyErr        *tls.CertificateVerificationError
	)

	result := func(text string, code int) (util.CurlFailure, bool) {
		return util.CurlFailure{Errno: curlePeerFailedVerify, Message: "SSL certificate OpenSSL verify result: " + text + " (" + strconv.Itoa(code) + ")", VerifyResult: code}, true
	}

	switch {
	case errors.Is(err, errChainTooLong):
		return result("certificate chain too long", 22)
	case errors.As(err, new(*peerNameError)):
		// lib/vtls/openssl.c, ossl_verifyhost (curlCheckPeerName)
		pe, _ := errors.AsType[*peerNameError](err)

		return util.CurlFailure{Errno: curlePeerFailedVerify, Message: pe.Error()}, true
	case errors.As(err, &unknownAuthority):
		var chain []*x509.Certificate
		if errors.As(err, &verifyErr) {
			chain = verifyErr.UnverifiedCertificates
		}

		switch {
		case len(chain) == 1 && isSelfSigned(chain[0]):
			return result("self-signed certificate", 18)
		case len(chain) > 1 && isSelfSigned(chain[len(chain)-1]):
			return result("self-signed certificate in certificate chain", 19)
		}

		return result("unable to get local issuer certificate", util.X509VErrUnableToGetIssuerCertLocally)
	case errors.As(err, &invalidCert):
		switch invalidCert.Reason {
		case x509.Expired:
			if invalidCert.Cert != nil && time.Now().Before(invalidCert.Cert.NotBefore) {
				return result("certificate is not yet valid", 9)
			}

			return result("certificate has expired", 10)
		case x509.IncompatibleUsage:
			return result("unsupported certificate purpose", 26)
		case x509.NotAuthorizedToSign:
			return result("invalid CA certificate", 24)
		case x509.TooManyIntermediates:
			return result("path length constraint exceeded", 25)
		}

		return result("certificate signature failure", 7)
	case errors.As(err, &verifyErr):
		return result("certificate signature failure", 7)
	}

	return util.CurlFailure{}, false
}

// isSelfSigned is OpenSSL's self-signed test: issued by its own subject,
// with a signature its own key verifies.
func isSelfSigned(c *x509.Certificate) bool {
	return string(c.RawIssuer) == string(c.RawSubject) && c.CheckSignature(c.SignatureAlgorithm, c.RawTBSCertificate, c.Signature) == nil
}

// opensslAlerts maps crypto/tls's text of an alert received from the peer
// to OpenSSL 3's error code and reason for it (SSL_AD_REASON_OFFSET plus
// the alert number).
var opensslAlerts = map[string][2]string{
	"unexpected message":              {"0A0003F2", "ssl/tls alert unexpected message"},
	"bad record MAC":                  {"0A0003FC", "ssl/tls alert bad record mac"},
	"decryption failed":               {"0A0003FD", "tlsv1 alert decryption failed"},
	"record overflow":                 {"0A0003FE", "tlsv1 alert record overflow"},
	"decompression failure":           {"0A000406", "ssl/tls alert decompression failure"},
	"handshake failure":               {"0A000410", "ssl/tls alert handshake failure"},
	"bad certificate":                 {"0A000412", "ssl/tls alert bad certificate"},
	"unsupported certificate":         {"0A000413", "ssl/tls alert unsupported certificate"},
	"revoked certificate":             {"0A000414", "ssl/tls alert certificate revoked"},
	"expired certificate":             {"0A000415", "ssl/tls alert certificate expired"},
	"unknown certificate":             {"0A000416", "ssl/tls alert certificate unknown"},
	"illegal parameter":               {"0A000417", "ssl/tls alert illegal parameter"},
	"unknown certificate authority":   {"0A000418", "tlsv1 alert unknown ca"},
	"access denied":                   {"0A000419", "tlsv1 alert access denied"},
	"error decoding message":          {"0A00041A", "tlsv1 alert decode error"},
	"error decrypting message":        {"0A00041B", "tlsv1 alert decrypt error"},
	"export restriction":              {"0A000424", "tlsv1 alert export restriction"},
	"protocol version not supported":  {"0A00042E", "tlsv1 alert protocol version"},
	"insufficient security level":     {"0A00042F", "tlsv1 alert insufficient security"},
	"internal error":                  {"0A000438", "tlsv1 alert internal error"},
	"inappropriate fallback":          {"0A00043E", "tlsv1 alert inappropriate fallback"},
	"user canceled":                   {"0A000442", "tlsv1 alert user cancelled"},
	"no renegotiation":                {"0A00044C", "tlsv1 alert no renegotiation"},
	"missing extension":               {"0A000455", "tlsv13 alert missing extension"},
	"unsupported extension":           {"0A000456", "tlsv1 unsupported extension"},
	"certificate unobtainable":        {"0A000457", "tlsv1 certificate unobtainable"},
	"unrecognized name":               {"0A000458", "tlsv1 unrecognized name"},
	"bad certificate status response": {"0A000459", "tlsv1 bad certificate status response"},
	"bad certificate hash value":      {"0A00045A", "tlsv1 bad certificate hash value"},
	"unknown PSK identity":            {"0A00045B", "tlsv1 alert unknown psk identity"},
	"certificate required":            {"0A00045C", "tlsv13 alert certificate required"},
	"no application protocol":         {"0A000460", "tlsv1 alert no application protocol"},
}

// opensslAlert is OpenSSL's error for an alert the peer sent, which
// crypto/tls reports as a "remote error".
func opensslAlert(err error) (code, reason string, ok bool) {
	for {
		opErr, isOp := errors.AsType[*net.OpError](err)
		if !isOp {
			return "", "", false
		}

		if opErr.Op == "remote error" && opErr.Err != nil {
			e, found := opensslAlerts[strings.TrimPrefix(opErr.Err.Error(), "tls: ")]

			return e[0], e[1], found
		}

		err = opErr.Err
	}
}

// streamWarnings are the warnings PHP 8.4's http stream wrapper raises
// when opening a stream fails as err does (checked against PHP on the
// same failures), for the failures it words differently from curl; nil
// for others. connected tells a failure after the connection was made.
func streamWarnings(err error, connected bool) []string {
	const (
		cryptoFailed = "Failed to enable crypto"
		openFailed   = "Failed to open stream: operation failed"
	)

	// php_openssl_handle_ssl_error: OpenSSL's error queue, or the socket
	// error, then php_openssl_enable_crypto's and the wrapper's failures
	sslFailure := func(first string) []string {
		if first == "" {
			return []string{cryptoFailed, openFailed}
		}

		return []string{first, cryptoFailed, openFailed}
	}

	var (
		unknownAuthority x509.UnknownAuthorityError
		invalidCert      x509.CertificateInvalidError
		nameErr          *peerNameError
	)

	switch {
	case errors.As(err, &nameErr) && nameErr.warning != "":
		// phpCheckPeerName
		return sslFailure(nameErr.warning)
	case errors.Is(err, errChainTooLong), errors.As(err, &unknownAuthority), errors.As(err, &invalidCert):
		return sslFailure("SSL operation failed with code 1. OpenSSL Error messages:\nerror:0A000086:SSL routines::certificate verify failed")
	case errors.Is(err, syscall.ECONNREFUSED):
		return []string{"Failed to open stream: Connection refused"}
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		// php_network_getaddresses: getaddrinfo()'s gai_strerror()
		msg := "php_network_getaddresses: getaddrinfo for " + dnsErr.Name + " failed: " + gaiStrerror(dnsErr)

		return []string{msg, "Failed to open stream: " + msg}
	}

	if _, ok := errors.AsType[tlsHandshakeTimeoutError](err); ok {
		return sslFailure("SSL: Handshake timed out")
	}

	if _, ok := errors.AsType[*tlsHandshakeError](err); ok {
		switch {
		case isConnReset(err):
			return sslFailure("SSL: Connection reset by peer")
		case errors.Is(err, syscall.EPIPE):
			return sslFailure("SSL: Broken pipe")
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return sslFailure("")
		}

		if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
			return sslFailure("SSL operation failed with code 1. OpenSSL Error messages:\nerror:0A00010B:SSL routines::wrong version number")
		}

		if code, reason, ok := opensslAlert(err); ok {
			return sslFailure("SSL operation failed with code 1. OpenSSL Error messages:\nerror:" + code + ":SSL routines::" + reason)
		}

		return nil
	}

	if !connected {
		if opErr := dialError(err); opErr != nil && opErr.Timeout() {
			return []string{"Failed to open stream: Connection timed out"}
		}

		return nil
	}

	// no response head: the server closed, reset or did not answer in time
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isConnReset(err) ||
		errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || isServerClosedIdle(err) {
		return []string{"Failed to open stream: HTTP request failed!"}
	}

	return nil
}

// serverClosedIdleText is the text of net/http's errServerClosedIdle,
// which it does not export; TestServerClosedIdleText fails when a Go
// release changes it.
const serverClosedIdleText = "http: server closed idle connection"

// isServerClosedIdle is net/http's error for a connection the server
// closed before the response began (errServerClosedIdle): to curl and the
// stream wrapper, a connection closed without a reply.
func isServerClosedIdle(err error) bool {
	return strings.Contains(err.Error(), serverClosedIdleText)
}

// gaiStrerror is the C library's gai_strerror() for a failed lookup:
// EAI_NONAME or EAI_AGAIN, worded as glibc does (BSD libc on macOS).
func gaiStrerror(err *net.DNSError) string {
	bsd := runtime.GOOS == "darwin" || strings.HasSuffix(runtime.GOOS, "bsd")

	switch {
	case err.IsTimeout || err.IsTemporary:
		return "Temporary failure in name resolution"
	case bsd:
		return "nodename nor servname provided, or not known"
	}

	return "Name or service not known"
}

// dialError is the failed dial in err, also when net/http reports it as
// the failure to reach a proxy ("proxyconnect").
func dialError(err error) *net.OpError {
	for {
		opErr, ok := errors.AsType[*net.OpError](err)
		if !ok {
			return nil
		}

		switch opErr.Op {
		case "dial":
			return opErr
		case "proxyconnect":
			err = opErr.Err
		default:
			return nil
		}
	}
}
