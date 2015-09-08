package rst

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"testing"
)

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

func TestCompressionFormat(t *testing.T) {
	r, _ := http.NewRequest("GET", "http://github.com", nil)

	r.Header.Set("Accept-Encoding", "gzip")
	if f := getCompressionFormat(testMBText, r); f != "gzip" {
		t.Fatal("Expected gzip value. Got:", f)
	}

	r.Header.Set("Accept-Encoding", "deflate")
	if f := getCompressionFormat(testMBText, r); f != "deflate" {
		t.Fatal("Expected deflate value. Got:", f)
	}

	r.Header.Set("Accept-Encoding", "gzip")
	if f := getCompressionFormat(testMBText[:CompressionThreshold-10], r); f != "" {
		t.Fatal("Expected no value. Got:", f)
	}
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

	// Accept-Encoding: some-unknown-format
	header.Set("Accept-Encoding", "some-unknown-format")
	rrUnknownFormat := newRequestResponse(Post, testEchoURL, header, bytes.NewReader(testMBText))
	if err := rrUnknownFormat.TestStatusCode(201); err != nil {
		t.Fatal("POST request:", err)
	}
	if err := rrUnknownFormat.TestHasNoHeader("Content-Encoding"); err != nil {
		t.Fatal("random Accept-Encoding value:", err)
	}
	if err := rrUnknownFormat.TestBody(bytes.NewReader(testMBText)); err != nil {
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
	if err := rrGzip.TestHeaderContains("Vary", "Accept-Encoding"); err != nil {
		t.Fatal("gzip Vary value:", err)
	}
	if decompressed, err := decompress(rrGzip.resp.Body, "gzip"); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(testMBText, decompressed) {
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
	if err := rrGzipNoThreshold.TestHasNoHeader("Content-Encoding"); err != nil {
		t.Fatal("gzip Accept-Encoding with small sized data:", err)
	}
	if err := rrGzipNoThreshold.TestBody(bytes.NewReader(testMBText[:size])); err != nil {
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
	if err := rrFlate.TestHeaderContains("Vary", "Accept-Encoding"); err != nil {
		t.Fatal("deflate Vary value:", err)
	}
	if decompressed, err := decompress(rrFlate.resp.Body, "deflate"); err != nil {
		t.Fatal(err)
	} else if !bytes.Equal(testMBText, decompressed) {
		t.Fatal("deflate Accept-Encoding value: data was decompressed but did not match the expected value")
	}
}
