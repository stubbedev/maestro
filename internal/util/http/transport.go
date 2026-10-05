// The net/http transfer engine behind CurlDownloader and RemoteFilesystem:
// what curl (and PHP's http stream wrapper) do for Composer, built on
// net/http. Errors are reported as curl reports them (errno and message),
// because Composer's messages, retries and hints are keyed on those.

package http

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/stubbedev/maestro/internal/util"
)

// curl error numbers Composer distinguishes.
const (
	curleUnsupportedProtocol = 1
	curleCouldntResolveHost  = 6
	curleCouldntConnect      = 7
	curleHTTP2               = 16
	curlePartialFile         = 18
	curleWriteError          = 23
	curleOperationTimedout   = 28
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
	// decode makes the transport undo gzip/deflate content encodings
	// (CURLOPT_ENCODING ""), advertising them unless a header does.
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
	// errno and errMsg are set when the transfer failed like curl fails.
	errno  int
	errMsg string
	// err, when set, rejects the job as it is (max size, blocked IP).
	err error

	status  int
	headers []string
	body    []byte
	info    util.TransferInfo
}

// transportPool builds and caches the transports of a downloader.
type transportPool struct {
	mu         sync.Mutex
	transports map[transportKey]*http.Client
}

func (p *transportPool) client(key transportKey, connectTimeout time.Duration) (*http.Client, *transferResult) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if c, ok := p.transports[key]; ok {
		return c, nil
	}

	tlsConfig, failure := buildTLSConfig(key.tls)
	if failure != nil {
		return nil, failure
	}

	network := "tcp"

	switch key.ipResolve {
	case 4:
		network = "tcp4"
	case 6:
		network = "tcp6"
	}

	dialer := &net.Dialer{Timeout: connectTimeout, KeepAlive: 60 * time.Second}

	t := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
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

	if key.proxy != "" {
		proxyURL, err := url.Parse(key.proxy)
		if err == nil {
			if key.proxyAuth != "" {
				user, pass, _ := strings.Cut(key.proxyAuth, ":")
				proxyURL.User = url.UserPassword(user, pass)
			}

			t.Proxy = http.ProxyURL(proxyURL)
		}

		if key.proxyHeader != "" {
			t.ProxyConnectHeader = http.Header{"Proxy-Authorization": {key.proxyHeader}}
		}
	}

	c := &http.Client{
		Transport: t,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	if p.transports == nil {
		p.transports = map[transportKey]*http.Client{}
	}

	p.transports[key] = c

	return c, nil
}

// buildTLSConfig turns ssl options into a tls.Config, failing like curl
// does for unusable CA or client certificate files.
func buildTLSConfig(s tlsSettings) (*tls.Config, *transferResult) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if s.cafile != "" || s.capath != "" {
		pool, err := loadCertPool(s.cafile, s.capath)
		if err != nil {
			return nil, &transferResult{errno: curleSSLCacertBadfile, errMsg: "error setting certificate verify locations:  CAfile: " + orNone(s.cafile) + " CApath: " + orNone(s.capath)}
		}

		cfg.RootCAs = pool
	}

	if s.localCert != "" {
		cert, err := loadClientCertificate(s.localCert, s.localPK, s.passphrase)
		if err != nil {
			return nil, &transferResult{errno: curleSSLCertproblem, errMsg: "could not load PEM client certificate from " + s.localCert + ", " + err.Error()}
		}

		cfg.Certificates = []tls.Certificate{cert}
	}

	switch {
	case !s.verifyPeer:
		cfg.InsecureSkipVerify = true // verify_peer false asks for exactly this
	case !s.verifyPeerName || s.allowSelfSigned:
		// Verify the chain ourselves: without the host name, or accepting a
		// self-signed leaf (allow_self_signed).
		cfg.InsecureSkipVerify = true // verification is done in VerifyConnection
		roots := cfg.RootCAs
		checkName := s.verifyPeerName
		selfSigned := s.allowSelfSigned
		cfg.VerifyConnection = func(cs tls.ConnectionState) error {
			return verifyChain(cs, roots, checkName, selfSigned)
		}
	}

	return cfg, nil
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}

	return s
}

// verifyChain is the peer verification of a connection whose standard
// verification was replaced.
func verifyChain(cs tls.ConnectionState, roots *x509.CertPool, checkName, allowSelfSigned bool) error {
	if len(cs.PeerCertificates) == 0 {
		return errors.New("tls: no peer certificates")
	}

	leaf := cs.PeerCertificates[0]
	opts := x509.VerifyOptions{Roots: roots, Intermediates: x509.NewCertPool()}

	if checkName {
		opts.DNSName = cs.ServerName
	}

	for _, c := range cs.PeerCertificates[1:] {
		opts.Intermediates.AddCert(c)
	}

	_, err := leaf.Verify(opts)
	if err != nil && allowSelfSigned && leaf.CheckSignatureFrom(leaf) == nil {
		if checkName {
			return leaf.VerifyHostname(cs.ServerName)
		}

		return nil
	}

	return err
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

// loadClientCertificate reads local_cert (which may hold the key too) and
// local_pk, decrypting a legacy encrypted PEM key with passphrase.
func loadClientCertificate(certFile, keyFile, passphrase string) (tls.Certificate, error) {
	certPEM, err := os.ReadFile(certFile)
	if err != nil {
		return tls.Certificate{}, err
	}

	keyPEM := certPEM
	if keyFile != "" {
		if keyPEM, err = os.ReadFile(keyFile); err != nil {
			return tls.Certificate{}, err
		}
	}

	if passphrase != "" {
		keyPEM = decryptPEMKeys(keyPEM, passphrase)
	}

	return tls.X509KeyPair(certPEM, keyPEM)
}

// decryptPEMKeys replaces legacy encrypted PEM private keys (Proc-Type:
// 4,ENCRYPTED) by their decryption with passphrase.
func decryptPEMKeys(data []byte, passphrase string) []byte {
	var out bytes.Buffer

	for {
		block, rest := pem.Decode(data)
		if block == nil {
			return out.Bytes()
		}

		//nolint:staticcheck // legacy PEM encryption is what OpenSSL-produced keys use
		if x509.IsEncryptedPEMBlock(block) {
			//nolint:staticcheck // see above
			if der, err := x509.DecryptPEMBlock(block, []byte(passphrase)); err == nil {
				block = &pem.Block{Type: block.Type, Bytes: der}
			}
		}

		_ = pem.Encode(&out, block)
		data = rest
	}
}

// do runs a transfer to completion. ctx cancels it (abortRequest).
func (p *transportPool) do(ctx context.Context, r *transferRequest) *transferResult {
	start := time.Now()
	res := &transferResult{info: util.TransferInfo{URL: r.url, DownloadContentLength: -1}}

	finish := func() *transferResult {
		res.info.TotalTime = time.Since(start).Seconds()
		if res.errno != 0 {
			res.info.ErrorCode = res.errno
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
	// every read.
	var idle *time.Timer
	if r.readTimeout > 0 {
		idle = time.AfterFunc(r.readTimeout, func() { cancelTransfer(context.DeadlineExceeded) })
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
		res.errno, res.errMsg = curleUnsupportedProtocol, err.Error()

		return finish()
	}

	var (
		connected atomic.Bool
		primaryIP atomic.Pointer[string]
	)

	host := req.URL.Hostname()
	port := req.URL.Port()

	if port == "" {
		port = "80"
		if req.URL.Scheme == "https" {
			port = "443"
		}
	}

	trace := &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connected.Store(true)

			if addr, ok := info.Conn.RemoteAddr().(*net.TCPAddr); ok {
				ip := addr.IP.String()
				primaryIP.Store(&ip)

				if r.preventIP != nil && r.preventIP(ip) {
					cancelTransfer(util.NewTransportError(`IP "`+ip+`" is blocked for "`+r.safeURL+`".`, 400))
				}
			}
		},
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

		res.errno, res.errMsg = curlError(ctx, err, host, port, connected.Load(), time.Since(start), 0)

		return finish()
	}
	defer resp.Body.Close()

	res.status = resp.StatusCode
	res.info.HTTPCode = resp.StatusCode
	res.info.DownloadContentLength = resp.ContentLength
	res.headers = responseHeaderLines(resp, r.curlStatusLines)

	if r.maxFileSize > 0 && resp.ContentLength > r.maxFileSize {
		res.err = util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Content-length header indicates " + strconv.FormatInt(resp.ContentLength, 10) + " bytes. Allowed " + strconv.FormatInt(r.maxFileSize, 10) + " bytes for " + r.safeURL)

		return finish()
	}

	counter := &countingReader{r: resp.Body, max: r.maxFileSize, idle: idle, idleTimeout: r.readTimeout}

	var src io.Reader = counter

	if r.decode {
		decoded, err := decodingReader(src, resp.Header.Get("Content-Encoding"))
		if err != nil {
			res.errno, res.errMsg = curleBadContentEncoding, "Error while processing content unencoding: "+err.Error()

			return finish()
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

	if err != nil {
		var wErr *writeError

		switch {
		case errors.Is(err, errMaxSize):
			res.err = util.NewMaxFileSizeExceededError("Maximum allowed download size reached. Downloaded " + strconv.FormatInt(counter.n, 10) + " of allowed " + strconv.FormatInt(r.maxFileSize, 10) + " bytes for " + r.safeURL)
		case errors.As(err, &wErr):
			res.errno, res.errMsg = curleWriteError, "Failure writing output to destination"
		default:
			if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) && !errors.Is(cause, context.DeadlineExceeded) {
				res.err = cause
			} else {
				res.errno, res.errMsg = curlError(ctx, err, host, port, true, time.Since(start), counter.n)
				if res.errno == curlePartialFile && resp.ContentLength > 0 {
					res.errMsg = "transfer closed with " + strconv.FormatInt(resp.ContentLength-counter.n, 10) + " bytes remaining to read"
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
	userAgent, accept, acceptEncoding := false, false, false

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

		switch strings.ToLower(name) {
		case "host":
			req.Host = value

			continue
		case "connection":
			// net/http manages the connection; Composer always asks for
			// keep-alive (curl) or close (streams).
			if strings.EqualFold(value, "close") {
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
			acceptEncoding = true
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

	if r.decode && !acceptEncoding {
		h["Accept-Encoding"] = []string{"deflate, gzip"}
	}

	if r.content != nil && h.Get("Content-Type") == "" {
		h["Content-Type"] = []string{"application/x-www-form-urlencoded"}
	}
}

// responseHeaderLines renders the response head as the header lines curl
// (or $http_response_header) holds: the status line, then the fields.
// net/http does not keep the order fields arrived in, so they are sorted
// by name; HTTP/2 names are lowercase as on the wire.
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
			shown = strings.ToLower(name)
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

// decodingReader undoes a Content-Encoding curl decodes.
func decodingReader(r io.Reader, encoding string) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return r, nil
	case "gzip", "x-gzip":
		return gzip.NewReader(r)
	case "deflate":
		// curl accepts zlib-wrapped and raw deflate.
		br := &peekReader{r: r}

		head, err := br.peek(2)
		if err == nil && len(head) == 2 && head[0]&0x0f == 8 && (uint16(head[0])<<8|uint16(head[1]))%31 == 0 {
			return zlib.NewReader(br)
		}

		return flate.NewReader(br), nil
	}

	return nil, errors.New("unsupported content encoding " + encoding)
}

// peekReader lets the first bytes be inspected before reading.
type peekReader struct {
	r    io.Reader
	head []byte
}

func (p *peekReader) peek(n int) ([]byte, error) {
	buf := make([]byte, n)
	m, err := io.ReadFull(p.r, buf)
	p.head = buf[:m]

	if errors.Is(err, io.ErrUnexpectedEOF) {
		err = nil
	}

	return p.head, err
}

func (p *peekReader) Read(b []byte) (int, error) {
	if len(p.head) > 0 {
		n := copy(b, p.head)
		p.head = p.head[n:]

		return n, nil
	}

	return p.r.Read(b)
}

// curlError maps a net/http failure to the curl error number and message
// curl reports for the same failure.
func curlError(ctx context.Context, err error, host, port string, connected bool, elapsed time.Duration, received int64) (int, string) {
	ms := strconv.FormatInt(elapsed.Milliseconds(), 10)

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		if !connected {
			return curleOperationTimedout, "Connection timed out after " + ms + " milliseconds"
		}

		return curleOperationTimedout, "Operation timed out after " + ms + " milliseconds with " + strconv.FormatInt(received, 10) + " bytes received"
	}

	if dnsErr, ok := errors.AsType[*net.DNSError](err); ok {
		if dnsErr.IsTimeout {
			return curleOperationTimedout, "Resolving timed out after " + ms + " milliseconds"
		}

		return curleCouldntResolveHost, "Could not resolve host: " + host
	}

	var (
		unknownAuthority x509.UnknownAuthorityError
		hostnameErr      x509.HostnameError
		invalidCert      x509.CertificateInvalidError
		verifyErr        *tls.CertificateVerificationError
	)

	switch {
	case errors.As(err, &hostnameErr):
		return curlePeerFailedVerify, "SSL: no alternative certificate subject name matches target host name '" + host + "'"
	case errors.As(err, &unknownAuthority):
		if c := unknownAuthority.Cert; c != nil && c.CheckSignatureFrom(c) == nil {
			return curlePeerFailedVerify, "SSL certificate problem: self-signed certificate"
		}

		return curlePeerFailedVerify, "SSL certificate problem: unable to get local issuer certificate"
	case errors.As(err, &invalidCert):
		if invalidCert.Reason == x509.Expired {
			return curlePeerFailedVerify, "SSL certificate problem: certificate has expired"
		}

		return curlePeerFailedVerify, "SSL certificate problem: " + invalidCert.Error()
	case errors.As(err, &verifyErr):
		return curlePeerFailedVerify, "SSL certificate problem: " + verifyErr.Err.Error()
	}

	if errors.Is(err, syscall.ECONNRESET) {
		if !connected {
			return curleSSLConnectError, "OpenSSL SSL_connect: Connection reset by peer in connection to " + host + ":" + port
		}

		return curleRecvError, "Recv failure: Connection reset by peer"
	}

	if errors.Is(err, syscall.EPIPE) {
		return curleSendError, "Send failure: Broken pipe"
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		reason := "Couldn't connect to server"
		if opErr.Timeout() {
			return curleOperationTimedout, "Failed to connect to " + host + " port " + port + " after " + ms + " ms: Timeout was reached"
		}

		return curleCouldntConnect, "Failed to connect to " + host + " port " + port + " after " + ms + " ms: " + reason
	}

	if _, ok := errors.AsType[tls.RecordHeaderError](err); ok {
		return curleSSLConnectError, "OpenSSL/3.0.0: error:0A00010B:SSL routines::wrong version number"
	}

	msg := err.Error()

	switch {
	case strings.Contains(msg, "stream error"):
		return curleHTTP2Stream, "HTTP/2 stream was not closed cleanly: " + msg
	case strings.Contains(msg, "http2:"):
		return curleHTTP2, "Error in the HTTP2 framing layer"
	case strings.Contains(msg, "tls:"):
		return curleSSLConnectError, "OpenSSL SSL_connect: " + msg + " in connection to " + host + ":" + port
	case errors.Is(err, io.ErrUnexpectedEOF):
		return curlePartialFile, "transfer closed with outstanding read data remaining"
	case errors.Is(err, io.EOF):
		if connected {
			return curleGotNothing, "Empty reply from server"
		}

		return curleCouldntConnect, "Failed to connect to " + host + " port " + port + " after " + ms + " ms: Couldn't connect to server"
	}

	return curleRecvError, "Failure when receiving data from the peer: " + msg
}
