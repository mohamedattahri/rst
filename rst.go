// Copyright (c) 2014, Mohamed Attahri

// Package rst implements tools and methods to expose resources in a RESTFul
// web service.
package rst

import (
	"compress/flate"
	"compress/gzip"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/context"
	gorillaMux "github.com/gorilla/mux"
)

const rfc1123 = "Mon, 02 Jan 2006 15:04:05 GMT"

// Common HTTP methods.
const (
	Options = "OPTIONS"
	Head    = "HEAD"
	Get     = "GET"
	Patch   = "PATCH"
	Put     = "PUT"
	Post    = "POST"
	Delete  = "DELETE"
)

const (
	gzipCompression  string = "gzip"
	flateCompression        = "deflate"
)

// CompressionThreshold is the minimal length that the body of a response must
// reach before compression is enabled.
// The current default value is the one used by Akamai, and falls within the
// range recommended by Google.
var CompressionThreshold = 860 // bytes

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

// RouteVars represents the variables extracted by the router from a URL.
type RouteVars map[string]string

// Get returns the value with key, or an empty string if not found.
func (rv RouteVars) Get(key string) string {
	value, _ := rv[key]
	return value
}

// ResponseWriter implements http.ResponseWriter, and adds data compression
// support.
type responseWriter struct {
	http.ResponseWriter
}

// Write will compress data in the format specified in the Content-Encoding
// header of the embedded http.ResponseWriter.
func (w *responseWriter) Write(b []byte) (int, error) {
	switch format := w.Header().Get("Content-Encoding"); format {
	case gzipCompression:
		compressor := gzip.NewWriter(w.ResponseWriter)
		defer compressor.Close()
		return compressor.Write(b)
	case flateCompression:
		compressor, _ := flate.NewWriter(w.ResponseWriter, 0)
		defer compressor.Close()
		return compressor.Write(b)
	case "":
		return w.ResponseWriter.Write(b)
	default:
		panic(fmt.Errorf("unsupported content encoding format %s", format))
	}
}

func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{w}
}

const varsKey = "__rest__vars"

func getVars(r *http.Request) (vars RouteVars) {
	if v := context.Get(r, varsKey); v != nil {
		vars = v.(RouteVars)
	}
	return vars
}
func setVars(r *http.Request, vars RouteVars) {
	context.Set(r, varsKey, vars)
}
func delVars(r *http.Request) {
	context.Clear(r)
}

// RESTMux represents a endpoint in an RESTful API.
type RESTMux struct {
	header http.Header
	ac     *AccessControlResponse
	m      *gorillaMux.Router
}

// NewRESTMux initializes a new REST multiplexer.
func NewRESTMux() *RESTMux {
	s := &RESTMux{
		header: make(http.Header),
		m:      gorillaMux.NewRouter(),
	}
	return s
}

// Header contains the headers that will automatically be set in all responses
// served from this mux.
func (s *RESTMux) Header() http.Header {
	return s.header
}

/*
SetCORSPolicy sets the access control parameters that will be used to write
CORS related headers. By default, CORS support is disabled.

Endpoints that implement Preflighter can customize the CORS headers returned
with the response to an HTTP OPTIONS preflight request.

The ac parameter can be DefaultAccessControl, PermissiveAccessControl, or a
custom defined AccessControlResponse struct. A nil value will disable support.
*/
func (s *RESTMux) SetCORSPolicy(ac *AccessControlResponse) {
	s.ac = ac
}

func (s *RESTMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Custom headers are written no matter what.
	for key, values := range s.header {
		for i, value := range values {
			if i == 0 {
				w.Header().Set(key, value)
			} else {
				w.Header().Add(key, value)
			}
		}
	}

	match := s.match(r)
	if match == nil || match.Handler == nil {
		NotFound().ServeHTTP(w, r)
		return
	}

	handler, validEndpoint := match.Handler.(*endpointHandler)
	setVars(r, RouteVars(match.Vars))
	defer delVars(r)

	if s.ac != nil && validEndpoint {
		newAccessControlHandler(handler.endpoint, s.ac).ServeHTTP(w, r)
	}
	handler.ServeHTTP(newResponseWriter(w), r)
}

// HandleEndpoint registers the endpoint for the given pattern.
// It's a shorthand for:
// 	s.Handle(pattern, EndpointHandler(endpoint))
func (s *RESTMux) HandleEndpoint(pattern string, endpoint Endpoint) {
	s.Handle(pattern, EndpointHandler(endpoint))
}

// Handle registers the handler function for the given pattern.
func (s *RESTMux) Handle(pattern string, handler http.Handler) {
	s.m.Handle(pattern, handler)
}

// match returns the route
func (s *RESTMux) match(r *http.Request) *gorillaMux.RouteMatch {
	var match gorillaMux.RouteMatch
	if !s.m.Match(r, &match) {
		return nil
	}
	return &match
}
