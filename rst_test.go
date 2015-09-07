package rst

import (
	"bufio"
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"
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

func (rr *requestResponse) TestHasNoHeader(key string) error {
	if err := rr.TestHasHeader(key); err == nil {
		return fmt.Errorf("not expecting to find header %s", key)
	}
	return nil
}

func (rr *requestResponse) TestDateHeader(key string, wanted time.Time) error {
	if rr.err != nil {
		return rr.err
	}

	if got, err := time.Parse(rfc1123, rr.resp.Header.Get(http.CanonicalHeaderKey(key))); err != nil {
		return err
	} else if !wanted.Equal(got) {
		return fmt.Errorf("header %s Wanted: %s Got: %s", key, wanted, got)
	}
	return nil
}

func (rr *requestResponse) TestHeader(key, value string) error {
	if rr.err != nil {
		return rr.err
	}

	if err := rr.TestHasHeader(key); err != nil {
		return err
	}

	values := rr.resp.Header[http.CanonicalHeaderKey(key)]
	for _, v := range values {
		if v == value {
			return nil
		}
	}
	return fmt.Errorf("header %s Wanted: %s Got: %s", key, value, values)
}

func (rr *requestResponse) TestHeaderContains(key, value string) error {
	if rr.err != nil {
		return rr.err
	}

	if err := rr.TestHasHeader(key); err != nil {
		return err
	}

	values := rr.resp.Header[http.CanonicalHeaderKey(key)]
	for _, v := range values {
		if strings.Contains(v, value) {
			return nil
		}
	}
	return fmt.Errorf("Could not find value \"%s\" in header \"%s\": \"%s\"", value, key, strings.Join(values, ", "))
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
	var (
		decompressor io.ReadCloser
	)

	switch format {
	case "gzip":
		reader, err := gzip.NewReader(src)
		if err != nil {
			return nil, err
		}
		decompressor = reader
	case "deflate":
		decompressor = flate.NewReader(src)
	default:
		panic(fmt.Errorf("unknown format %s", format))
	}

	defer decompressor.Close()
	buffer := new(bytes.Buffer)
	io.Copy(buffer, decompressor)
	return buffer.Bytes(), nil
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

func TestBypass(t *testing.T) {
	rr := newRequestResponse(Post, testServerAddr+"/bypass", nil, nil)
	if err := rr.TestBody(bytes.NewBuffer(testCannedBytes)); err != nil {
		t.Fatal(err)
	}
}

func TestMapEndpoint(t *testing.T) {
	mendp := make(mapEndpoint)
	_ = Getter(mendp)
	_ = Poster(mendp)
	_ = Putter(mendp)
	_ = Patcher(mendp)
	_ = Deleter(mendp)
}

func TestMuxMethodHandlers(t *testing.T) {
	testMux.Get("/muxMethodHandler/{name}", func(vars RouteVars, r *http.Request) (Resource, error) {
		return nil, nil
	})
	testMux.Post("/muxMethodHandler/{name}", func(vars RouteVars, r *http.Request) (Resource, string, error) {
		return nil, "", nil
	})

	rr := newRequestResponse(Options, testServerAddr+"/muxMethodHandler/blabla", nil, nil)
	if err := rr.TestHeaderContains("Allow", Head); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHeaderContains("Allow", Get); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHeaderContains("Allow", Post); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHeaderContains("Allow", Patch); err == nil {
		t.Fatal("Patch not expected to be found in headers")
	}
	if err := rr.TestHeaderContains("Allow", Put); err == nil {
		t.Fatal("Patch not expected to be found in headers")
	}
	if err := rr.TestHeaderContains("Allow", Delete); err == nil {
		t.Fatal("Patch not expected to be found in headers")
	}

	testMux.Put("/employers/{name}", func(vars RouteVars, r *http.Request) (Resource, error) {
		return nil, nil
	})
	testMux.Patch("/employers/{name}", func(vars RouteVars, r *http.Request) (Resource, error) {
		return nil, nil
	})
	testMux.Delete("/employers/{name}", func(vars RouteVars, r *http.Request) error {
		return nil
	})
}

func TestResponseCompression(t *testing.T) {
	header := make(http.Header)

	// Accept-Encoding: none
	rr0 := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rr0.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rr0.TestHasNoHeader("Content-Encoding"); err != nil {
		t.Fatal("no Accept-Encoding header:", err)
	}
	canonical, _ := ioutil.ReadAll(rr0.resp.Body)
	defer rr0.resp.Body.Close()

	// Accept-Encoding: some-unknown-format
	header.Set("Accept-Encoding", "some-unknown-format")
	rrUnknownFormat := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rrUnknownFormat.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrUnknownFormat.TestHasNoHeader("Content-Encoding"); err != nil {
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

func TestEnvelope(t *testing.T) {
	var test = func(accept string, body io.Reader) {
		rr := newRequestResponse(Get, testEnvelopeURL, http.Header{"Accept": []string{accept}}, nil)

		if err := rr.TestStatusCode(http.StatusOK); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestHeader("ETag", envelopeETag); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestDateHeader("Last-Modified", envelopeLastModified); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestHasHeader("Expires"); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestHeaderContains("Content-Type", accept); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestHeader("X-Envelope-Header", envelopeHeaders.Get("X-Envelope-Header")); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestBody(body); err != nil {
			t.Fatal(err)
		}
	}

	b, _ := json.Marshal(envelopeProjection)
	test("application/json", bytes.NewReader(b))
	test("text/plain", bytes.NewReader([]byte(envelopeTextProjection)))
}
