// Copyright (c) 2014, Mohamed Attahri

/*
Package rst implements tools and methods to expose resources in a RESTFul
web service.

The idea behind rst is to have endpoints and resources implementing interfaces to add features.

Endpoints can implement Getter, Poster, Patcher, Putter or Deleter to respectively allow the HEAD/GET, POST, PATCH, PUT, and DELETE HTTP methods.

Resources can implement Ranger to support partial GET requests, or Marshaler to customize the process with which they are encoded.

With these interfaces, the complexity behind dealing with all the headers and status codes of the HTTP protocol is abstracted to let you focus on returning a resource or an error.

Resources

A resource must implement the Resource interface. Here's a basic example:

	type Person struct {
		ID string
		Name string
		ModifiedDate time.Time `json:"-" xml:"-"`
	}

	// This will be helpful for conditional GETs
	// and to detect conflicts before PATCHs for example.
	func (p *Person) LastModified() time.Time {
		return p.ModifiedDate
	}

	// An ETag inspired by Facebook.
	func (p *Person) ETag() string {
		return fmt.Sprintf("%d-%s", p.LastModified().Unix(), p.ID)
	}

	// This value will help set the Expires header and
	// improve the cacheability of this resource.
	func (p *Person) TTL() time.Duration {
		return 10 * time.Second
	}

Endpoints

An endpoint is an access point to a resource in your service.

In the following example, PersonEP implements Getter and is therefore able to handle GET requests.

	type PersonEP struct {}

	func (ep *PersonEP) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}
		return resource, nil
	}

Get uses the id variable extracted from the URL to load a resource from the database, or return a 404 Not Found error.

Routing

Routing of requests in rst is powered by Gorilla mux (https://github.com/gorilla/mux). Only URL patterns are available for now. Optional regular expressions are supported.

	mux := rst.NewMux()

	// Headers set in mux are added to all responses
	mux.Header().Set("Server", "Awesome Service Software 1.0")
	mux.Header().Set("X-Powered-By", "rst")

	mux.Handle("/people/{id:\\d+}", rst.EndpointHandler(&PersonEP{}))

	http.ListenAndServe(":8080", mux)

At this point, our service only allows `GET` requests on a resource called `Person`.

Encoding

rst supports JSON, XML and text encoding of resources using the encoders in Go's standard library.

It negotiates the right encoding format based on the content of the Accept header in the request, calls the appropriate marshaler, and inserts the result in a response with the right status code and headers.

You can implement the Marshaler interface if you want to add support for another format, or for more control over the encoding process of a specific resource.

Compression

rst compresses the payload of responses using the supported algorithm detected in the request's Accept-Encoding header.

Payloads under CompressionThreshold bytes are not compressed.

Both Gzip and Flate are supported.
*/
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

// Mux is an HTTP request multiplexer. It matches the URL of each incoming
// requests against a list of registered REST endpoints.
type Mux struct {
	header http.Header
	ac     *AccessControlResponse
	m      *gorillaMux.Router
}

// NewMux initializes a new REST multiplexer.
func NewMux() *Mux {
	s := &Mux{
		header: make(http.Header),
		m:      gorillaMux.NewRouter(),
	}
	return s
}

// Header contains the headers that will automatically be set in all responses
// served from this mux.
func (s *Mux) Header() http.Header {
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
func (s *Mux) SetCORSPolicy(ac *AccessControlResponse) {
	s.ac = ac
}

func (s *Mux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
func (s *Mux) HandleEndpoint(pattern string, endpoint Endpoint) {
	s.Handle(pattern, EndpointHandler(endpoint))
}

// Handle registers the handler function for the given pattern.
func (s *Mux) Handle(pattern string, handler http.Handler) {
	s.m.Handle(pattern, handler)
}

// match returns the route
func (s *Mux) match(r *http.Request) *gorillaMux.RouteMatch {
	var match gorillaMux.RouteMatch
	if !s.m.Match(r, &match) {
		return nil
	}
	return &match
}
