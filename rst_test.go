package rst

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

func newRequest(s string) (*http.Request, error) {
	return http.ReadRequest(bufio.NewReader(bytes.NewReader([]byte(s))))
}

type requestResponse struct {
	req  *http.Request
	resp *http.Response
	err  error
}

func (rr *requestResponse) TestStatusCode(code int) error {
	if rr.err != nil {
		return rr.err
	}

	if rr.resp.StatusCode != code {
		return fmt.Errorf("status code wanted: %d (%s) Got: %d (%s)", code, http.StatusText(code), rr.resp.StatusCode, http.StatusText(rr.resp.StatusCode))
	}

	return nil
}

func (rr *requestResponse) TestHasHeader(key string) error {
	if _, exists := rr.resp.Header[http.CanonicalHeaderKey(key)]; !exists {
		return fmt.Errorf("expected to find header %s", key)
	}
	return nil
}

func (rr *requestResponse) TestHeader(key, value string) error {
	if rr.err != nil {
		return rr.err
	}

	if v := rr.resp.Header.Get(http.CanonicalHeaderKey(key)); v != value {
		return fmt.Errorf("header %s Wanted: %s Got: %s", key, value, v)
	}
	return nil
}

func (rr *requestResponse) TestHeaderContains(key, value string) error {
	if rr.err != nil {
		return rr.err
	}

	if v := rr.resp.Header.Get(http.CanonicalHeaderKey(key)); !strings.Contains(v, value) {
		return fmt.Errorf("header content %s Wanted: %s Got: %s", key, value, v)
	}
	return nil
}

func (rr *requestResponse) TestBody(reader io.Reader) error {
	if rr.err != nil {
		return rr.err
	}

	wanted, werr := ioutil.ReadAll(reader)
	if werr != nil {
		return werr
	}

	got, gerr := ioutil.ReadAll(rr.resp.Body)
	if gerr != nil {
		return gerr
	}
	defer rr.resp.Body.Close()

	if !bytes.Equal(wanted, got) {
		return fmt.Errorf("bodies did not match. Wanted: %d Got: %d", len(wanted), len(got))
	}
	return nil
}

func newRequestResponse(method, url string, header http.Header, data io.Reader) *requestResponse {
	client := &http.Client{}
	req, _ := http.NewRequest(method, url, data)
	if header != nil {
		req.Header = header
	}
	resp, err := client.Do(req)
	return &requestResponse{req, resp, err}
}

func decompress(src io.ReadCloser, format string) ([]byte, error) {
	var decompressor io.ReadCloser
	var err error

	switch format {
	case "gzip":
		decompressor, err = gzip.NewReader(src)
	case "deflate":
		decompressor = flate.NewReader(src)
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}

	if err != nil {
		return nil, err
	}
	defer src.Close()
	defer decompressor.Close()
	return ioutil.ReadAll(decompressor)
}

func TestMuxHeaders(t *testing.T) {
	header, value := "X-Custom-Header", "hello, world!"

	var test = func(addr string) {
		resp, err := http.Get(addr)
		if err != nil {
			t.Fatal(err)
		}

		if v := resp.Header.Get(header); v != value {
			t.Fatal("Got:", v, "Wanted:", value, "for header named", header)
		}
	}

	testMux.Header().Add(header, value)
	defer testMux.Header().Del(header)
	test(testSafeURL)              // 200 OK
	test(testServerAddr + "/manu") // 404 NOT FOUND
}

func TestResponseCompression(t *testing.T) {
	header := make(http.Header)

	// Accept-Encoding: none
	rr0 := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rr0.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rr0.TestHeader("Content-Encoding", ""); err != nil {
		t.Fatal("no Accept-Encoding header:", err)
	}
	canonical, _ := ioutil.ReadAll(rr0.resp.Body)
	rr0.resp.Body.Close()

	// Accept-Encoding: some-unknown-format
	header.Set("Accept-Encoding", "some-unknown-format")
	rrUnknownFormat := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rrUnknownFormat.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrUnknownFormat.TestHeader("Content-Encoding", ""); err != nil {
		t.Fatal("random Accept-Encoding value:", err)
	}
	if err := rrUnknownFormat.TestBody(bytes.NewReader(canonical)); err != nil {
		t.Fatal("random Accept-Encoding value:", err)
	}

	// Accept-Encoding: gzip
	header.Set("Accept-Encoding", "gzip")
	rrGzip := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rrGzip.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrGzip.TestHeader("Content-Encoding", "gzip"); err != nil {
		t.Fatal("gzip Accept-Encoding value:", err)
	}
	if decompressed, err := decompress(rrGzip.resp.Body, "gzip"); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(canonical, decompressed) {
		t.Fatal("gzip Accept-Encoding value: data was decompressed but did not match the expected value")
	}

	// Accept-Encoding: gzip (< CompressionThreshold)
	header.Set("Accept-Encoding", "gzip")
	size := CompressionThreshold - 10
	buffer := bytes.NewBuffer(testMBText[:size])
	rrGzipNoThreshold := newRequestResponse(Post, testEchoURL, header, buffer)
	if err := rrGzipNoThreshold.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrGzipNoThreshold.TestHeader("Content-Encoding", ""); err != nil {
		t.Fatal("gzip Accept-Encoding with small sized data:", err)
	}
	if err := rrGzipNoThreshold.TestBody(bytes.NewReader(canonical[:size])); err != nil {
		t.Fatal("gzip Accept-Encoding with small sized data:", err)
	}

	// Accept-Encoding: deflate
	header.Set("Accept-Encoding", "deflate")
	rrFlate := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rrFlate.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrFlate.TestHeader("Content-Encoding", "deflate"); err != nil {
		t.Fatal("deflate Accept-Encoding value:", err)
	}
	if decompressed, err := decompress(rrFlate.resp.Body, "deflate"); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(canonical, decompressed) {
		t.Fatal("deflate Accept-Encoding value: data was decompressed but did not match the expected value")
	}
}
