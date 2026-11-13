// Ports tests/Composer/Test/Util/Http/CurlDownloaderTest.php (the URL
// errors, access callables, max_file_size and stall timeout).

package http

import (
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stubbedev/maestro/internal/console"
	mio "github.com/stubbedev/maestro/internal/io"
	"github.com/stubbedev/maestro/internal/php"
	"github.com/stubbedev/maestro/internal/phperr"
	"github.com/stubbedev/maestro/internal/util"
)

func createLocalServer(t *testing.T) net.Listener {
	t.Helper()

	clearProxyEnv(t)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("Could not open a local socket: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })

	return l
}

func acceptClient(t *testing.T, l net.Listener) net.Conn {
	t.Helper()

	tl, _ := l.(*net.TCPListener)
	if err := tl.SetDeadline(time.Now().Add(time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	conn, err := l.Accept()
	if err != nil {
		return nil
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func tickUntil(t *testing.T, c *CurlDownloader, condition func() bool) {
	t.Helper()

	c.selectTimeout = 10 * time.Millisecond
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("Timed out waiting for the transfer")
		}
		c.Tick()
	}

	for range 10 {
		c.Tick()
	}
}

func jobCount(c *CurlDownloader) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return len(c.jobs)
}

func download(t *testing.T, c *CurlDownloader, reject func(error), url string, options *php.Array) {
	t.Helper()

	if _, err := c.Download(func(*Response) {}, reject, "localhost", url, options, ""); err != nil {
		t.Fatal(err)
	}
}

func TestCurlDownloader_CurlErrorForAUrlCurlCannotParseNamesTheRequestedUrl(t *testing.T) {
	c, _ := newTestCurlDownloader(t, nil)
	url := "https://example.invalid/foo bar"

	var got error
	if _, err := c.Download(func(*Response) {
		t.Error("The download of a malformed URL should not succeed")
	}, func(e error) { got = e }, "example.invalid", url, php.NewArray(), ""); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 100 && got == nil; i++ {
		c.Tick()
	}

	if _, ok := errors.AsType[*util.TransportError](got); !ok {
		t.Fatalf("expected a TransportException, got %v", got)
	}
	if !strings.Contains(got.Error(), "while downloading "+url+":") {
		t.Errorf("got %q", got.Error())
	}
}

func TestCurlDownloader_RequestToABlockedIpIsRemovedFromTheTransfersInFlight(t *testing.T) {
	server := createLocalServer(t)
	c, _ := newTestCurlDownloader(t, nil)

	var errs []error
	download(t, c, func(e error) { errs = append(errs, e) }, "http://"+server.Addr().String()+"/", php.ArrayOf("prevent_ip_access_callable", AccessCallable(func(string) (bool, error) { return true, nil })))

	tickUntil(t, c, func() bool {
		acceptClient(t, server)

		return len(errs) > 0
	})

	if len(errs) != 1 {
		t.Fatalf("got %d errors: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Error(), "is blocked for") {
		t.Errorf("got %q", errs[0].Error())
	}
	if n := jobCount(c); n != 0 {
		t.Errorf("a rejected request must not keep transferring, %d jobs left", n)
	}
}

func TestCurlDownloader_RequestToABlockedIpIsNotSent(t *testing.T) {
	server := createLocalServer(t)
	c, _ := newTestCurlDownloader(t, nil)

	var errs []error
	download(t, c, func(e error) { errs = append(errs, e) }, "http://"+server.Addr().String()+"/", php.ArrayOf("prevent_ip_access_callable", AccessCallable(func(ip string) (bool, error) { return ip == "127.0.0.1", nil })))

	var client net.Conn
	tickUntil(t, c, func() bool {
		if client == nil {
			client = acceptClient(t, server)
		}

		return len(errs) > 0
	})
	if client == nil {
		client = acceptClient(t, server)
	}

	if len(errs) != 1 {
		t.Fatalf("got %d errors: %v", len(errs), errs)
	}
	if _, ok := errors.AsType[*util.TransportError](errs[0]); !ok {
		t.Fatalf("expected a TransportException, got %v", errs[0])
	}
	if want := `IP "127.0.0.1" is blocked for "http://` + server.Addr().String() + `/".`; errs[0].Error() != want {
		t.Errorf("got %q, want %q", errs[0].Error(), want)
	}
	if client == nil {
		t.Fatal("the connection was never made")
	}

	if err := client.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	sent, err := io.ReadAll(client)
	if len(sent) != 0 {
		t.Errorf("nothing must be sent to a blocked IP, got %q", sent)
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Error("the connection must have been closed")
	}
}

func TestCurlDownloader_ExceptionFromThePreventIpAccessCallableRejectsTheRequest(t *testing.T) {
	server := createLocalServer(t)
	c, _ := newTestCurlDownloader(t, nil)
	exception := &util.RuntimeError{Message: "Could not check the IP"}

	var errs []error
	download(t, c, func(e error) { errs = append(errs, e) }, "http://"+server.Addr().String()+"/", php.ArrayOf("prevent_ip_access_callable", AccessCallable(func(string) (bool, error) { return false, exception })))

	tickUntil(t, c, func() bool {
		acceptClient(t, server)

		return len(errs) > 0
	})

	if len(errs) != 1 || !errors.Is(errs[0], exception) {
		t.Fatalf("got %v, want [%v]", errs, exception)
	}
	if n := jobCount(c); n != 0 {
		t.Errorf("%d jobs left", n)
	}
}

func TestCurlDownloader_RequestExceedingTheMaxFileSizeIsRejectedOnceAndRemovedFromTheTransfersInFlight(t *testing.T) {
	server := createLocalServer(t)
	c, _ := newTestCurlDownloader(t, nil)

	var errs []error
	download(t, c, func(e error) { errs = append(errs, e) }, "http://"+server.Addr().String()+"/", php.ArrayOf("max_file_size", 10))

	tickUntil(t, c, func() bool {
		if client := acceptClient(t, server); client != nil {
			_, _ = client.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n" + strings.Repeat("a", 500)))
		}

		return len(errs) > 0
	})

	if len(errs) != 1 {
		t.Fatalf("got %d errors: %v", len(errs), errs)
	}
	if _, ok := errors.AsType[*util.MaxFileSizeExceededError](errs[0]); !ok {
		t.Errorf("expected a MaxFileSizeExceededException, got %v", errs[0])
	}
	if n := jobCount(c); n != 0 {
		t.Errorf("a rejected request must not keep transferring, %d jobs left", n)
	}
}

func TestCurlDownloader_RejectionWhichAbortsAnotherTransferDoesNotBreakTheProgressLoop(t *testing.T) {
	server := createLocalServer(t)
	c, _ := newTestCurlDownloader(t, nil)

	var errs []error
	reject := func(e error) {
		errs = append(errs, e)
		for id := range c.jobs {
			c.abortRequest(id)
		}
	}
	url := "http://" + server.Addr().String() + "/"
	download(t, c, reject, url, php.ArrayOf("max_file_size", 10))
	download(t, c, reject, url, php.ArrayOf("max_file_size", 10))

	var clients []net.Conn
	sent := false
	tickUntil(t, c, func() bool {
		for len(clients) < 2 {
			client := acceptClient(t, server)
			if client == nil {
				break
			}
			clients = append(clients, client)
		}
		if !sent && len(clients) == 2 {
			for _, client := range clients {
				_, _ = client.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 1000\r\n\r\n"))
			}
			sent = true
		}

		return len(errs) > 0
	})

	if len(errs) != 1 {
		t.Fatalf("got %d errors: %v", len(errs), errs)
	}
	if n := jobCount(c); n != 0 {
		t.Errorf("%d jobs left", n)
	}
}

func TestCurlDownloader_StallTimeoutCanBeConfiguredViaEnv(t *testing.T) {
	t.Setenv("COMPOSER_HTTP_STALL_TIMEOUT", "0")
	if c, _ := newTestCurlDownloader(t, nil); c.stallTimeout != 0 {
		t.Errorf("got %v, want 0", c.stallTimeout)
	}

	t.Setenv("COMPOSER_HTTP_STALL_TIMEOUT", "garbage")
	if c, _ := newTestCurlDownloader(t, nil); c.stallTimeout != 30*time.Second {
		t.Errorf("got %v, want 30s", c.stallTimeout)
	}
}

func TestCurlDownloader_ServerWhichNeverAnswersIsGivenUpOnBeforeTheTransferTimeout(t *testing.T) {
	server := createLocalServer(t)
	b, err := mio.NewBufferIO("", console.VerbosityDebug, nil)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := newTestCurlDownloader(t, b)
	c.stallTimeout = time.Second

	url := "http://" + server.Addr().String() + "/packages.json"
	if _, err := c.Download(func(*Response) {}, func(error) {}, server.Addr().String(), url, php.NewArray(), ""); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(b.Output(), "Retrying (1)") {
		c.Tick()
	}

	c.mu.Lock()
	for id := range c.jobs {
		c.abortRequest(id)
	}
	c.mu.Unlock()

	if want := "Retrying (1) " + url + " due to curl error 28"; !strings.Contains(b.Output(), want) {
		t.Errorf("output %q does not contain %q", b.Output(), want)
	}
}

func TestCurlDownloader_AccessCallableOptionWhichIsNotAClosureIsRejected(t *testing.T) {
	for _, option := range []string{"prevent_url_access_callable", "prevent_ip_access_callable"} {
		c, _ := newTestCurlDownloader(t, nil)

		_, err := c.Download(func(*Response) {}, func(error) {}, "example.org", "https://example.org/packages.json", php.ArrayOf(option, "is_string"), "")
		if !phperr.InstanceOf(err, "InvalidArgumentException") {
			t.Fatalf("%s: expected an InvalidArgumentException, got %v", option, err)
		}
		if want := `The "` + option + `" transport option must be a Closure, got string.`; err.Error() != want {
			t.Errorf("got %q, want %q", err.Error(), want)
		}
	}
}
