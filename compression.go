package rst

import (
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	gzipCompression  string = "gzip"
	flateCompression        = "deflate"
)

var (
	// CompressionThreshold is the minimal length of the data to send in the
	// response ResponseWriter must reach before compression is enabled.
	// The current default value is the one used by Akamai, and falls within the
	// range recommended by Google.
	CompressionThreshold = 860 // bytes

	// errUnknownCompressionFormat is returned when the format of compression
	// required in unknown.
	errUnknownCompressionFormat = errors.New("unsupported compression format")

	// gZipCompressorPool allows rst to recyle gzip writers.
	gZipCompressorPool = sync.Pool{
		New: func() interface{} {
			return gzip.NewWriter(nil)
		},
	}
	// flateCompressorPool allows rst to recyle flate writers.
	flateCompressorPool = sync.Pool{
		New: func() interface{} {
			writer, _ := flate.NewWriter(nil, 0)
			return writer
		},
	}
)

// getCompressionFormat returns the compression for that will be used for b as
// a payload in the response to r. The returned string is either empty, gzip, or
// deflate.
func getCompressionFormat(b []byte, r *http.Request) string {
	if b == nil || len(b) < CompressionThreshold {
		return ""
	}

	encoding := r.Header.Get("Accept-Encoding")
	if strings.Contains(encoding, gzipCompression) {
		return gzipCompression
	}
	if strings.Contains(encoding, flateCompression) {
		return flateCompression
	}
	return ""
}

// compressor defines the methods implements by a compression writer.
type compressor interface {
	io.Writer
	Flush() error
	Reset(io.Writer)
}

// getCompressor returns a writer that can compress data written to it.
func compress(format string, dest io.Writer, b []byte) (int, error) {
	var writer compressor
	switch format {
	case gzipCompression:
		writer = gZipCompressorPool.Get().(*gzip.Writer)
		defer gZipCompressorPool.Put(writer)
	case flateCompression:
		writer = flateCompressorPool.Get().(*flate.Writer)
		defer flateCompressorPool.Put(writer)
	default:
		return 0, errUnknownCompressionFormat
	}

	writer.Reset(dest)
	n, err := writer.Write(b)
	writer.Flush()
	return n, err
}
