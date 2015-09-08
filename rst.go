// Copyright (c) 2014, Mohamed Attahri

// rst relies on esc (https://github.com/mjibson/esc) to embed static resources
// with go generate (requires go 1.4 or more recent).
//go:generate esc -pkg=assets -o=internal/assets/assets.go ./internal/assets

/*
Package rst implements tools and methods to expose resources in a RESTFul
web service.

The idea behind rst is to have endpoints and resources implement interfaces to
support HTTP features.

Endpoints can implement Getter, Poster, Patcher, Putter or Deleter to
respectively allow the HEAD/GET, POST, PATCH, PUT, and DELETE HTTP methods.

Resources can implement Ranger to support partial GET requests, Marshaler to
customize the process with which they are encoded, or http.Handler to have a
complete control over the ResponseWriter.

With these interfaces, the complexity behind dealing with all the headers and
status codes of the HTTP protocol is abstracted to let you focus on returning a
resource or an error.

Resources

A resource must implement the rst.Resource interface.

For that, you can either wrap an rst.Envelope around an existing type, or
define a new type and implement the methods of the interface yourself.

Using a rst.Envelope:

	projection := map[string]string{
		"ID"	: "a1-b2-c3-d4-e5-f6",
		"Name"	: "Francis Underwood",
	}
	lastModified := time.Now()
	etag := fmt.Sprintf("%d-%s", lastModified.Unix(), projection["ID"])
	ttl = 10 * time.Minute

	resource := rst.NewEnvelope(
		projection,
		lastModified,
		etag,
		ttl,
	)

Using a struct:

	type Person struct {
		ID string
		Name string
		modifiedDate time.Time
	}

	// This will be helpful for conditional GETs
	// and to detect conflicts before PATCHs for example.
	func (p *Person) LastModified() time.Time {
		return p.modifiedDate
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

	resource := &Person{
		ID: "a1-b2-c3-d4-e5-f6",
		Name: "Francis Underwood",
		modifiedDate: time.Now(),
	}

Endpoints

An endpoint is an access point to a resource in your service.

You can either define an endpoint by defining handlers for different methods
sharing the same pattern, or by submitting a type that implements Getter, Poster,
Patcher, Putter, Deleter and/or Prefligher.

Using rst.Mux:

	mux := rst.NewMux()
	mux.Get("/people/{id:\\d+}", func(vars RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nul, rst.NotFound()
		}
		return resource, nil
	})
	mux.Delete("/people/{id:\\d+}", func(vars RouteVars, r *http.Request) error {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nul, rst.NotFound()
		}
		return resource.Delete()
	})

Using a struct:

In the following example, PersonEP implements Getter and is therefore able to
handle GET requests.

	type PersonEP struct {}

	func (ep *PersonEP) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}
		return resource, nil
	}

	func (ep *PersonEP) Delete(vars rst.RouteVars, r *http.Request) error {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}
		return resource.Delete()
	}

Routing

Routing of requests in rst is powered by Gorilla mux
(https://github.com/gorilla/mux). Only URL patterns are available for now.
Optional regular expressions are supported.

	mux := rst.NewMux()
	mux.Debug = true // make sure this is switched back to false before production

	// Headers set in mux are added to all responses
	mux.Header().Set("Server", "Awesome Service Software 1.0")
	mux.Header().Set("X-Powered-By", "rst")

	mux.Handle("/people/{id:\\d+}", rst.EndpointHandler(&PersonEP{}))

	http.ListenAndServe(":8080", mux)

Encoding

rst supports JSON, XML and text encoding of resources using the encoders in Go's
standard library.

It negotiates the right encoding format based on the content of the Accept
header in the request, calls the appropriate marshaler, and inserts the result
in a response with the right status code and headers.

You can implement the Marshaler interface if you want to add support for another
format, or for more control over the encoding process of a specific resource.

Compression

rst compresses the payload of responses using the supported algorithm detected
in the request's Accept-Encoding header.

Payloads under the size defined in the CompressionThreshold const are not compressed.

Both Gzip and Flate are supported.

Options

OPTIONS requests are implicitly supported by all endpoints.

Cache

The ETag, Last-Modified and Vary headers are automatically set.

rst responds with 304 NOT MODIFIED when an appropriate If-Modified-Since or
If-None-Match header is found in the request.

The Expires header is also automatically inserted with the duration returned by
Resource.TTL().

Partial Gets

A resource can implement the Ranger interface to gain the ability to return
partial responses with status code 206 PARTIAL CONTENT and Content-Range
header automatically inserted.

Ranger.Range method will be called when a valid Range header is found in an
incoming GET request.

The Accept-Range header will be inserted automatically.

The supported range units and the range extent will be validated for you.

Note that the If-Range conditional header is supported as well.

CORS

rst can add the headers required to serve cross-origin (CORS) requests for you.

You can choose between two provided policies (DefaultAccessControl and
PermissiveAccessControl), or define your own.

	mux.SetCORSPolicy(rst.PermissiveAccessControl)

Support can be disabled by passing nil.

Preflighted requests are also supported. However, you can customize the
responses returned by preflight OPTIONS requests if you implement the
Preflighter interface in your endpoint.
*/
package rst

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gorilla/context"
	gorillaMux "github.com/gorilla/mux"
)

// rfc1123 with GMT
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
	wfl io.Writer
}

// Flush sends content down the transport.
func (rw *responseWriter) flush() {
	if rw.wfl == nil {
		return
	}

	if compressor, ok := rw.wfl.(compressor); ok {
		compressor.Flush()
		return
	}

	if flusher, ok := rw.wfl.(http.Flusher); ok {
		flusher.Flush()
		return
	}
}

// Write will compress data in the format specified in the Content-Encoding
// header of the embedded http.ResponseWriter.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := compress(rw.ResponseWriter.Header().Get("Content-Encoding"), rw.ResponseWriter, b)
	if err == errUnknownCompressionFormat {
		return rw.ResponseWriter.Write(b)
	}
	return n, err
}

// newResponseWriter returns an enhanced implementation of http.ResponseWriter.
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w}
}

const varsKey = "__rst__vars"

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
	Debug     bool // Set to true to display stack traces and debug info in errors.
	Logger    *log.Logger
	header    http.Header
	ac        *AccessControlResponse
	m         *gorillaMux.Router
	endpoints map[string]mapEndpoint
}

// NewMux initializes a new REST multiplexer.
func NewMux() *Mux {
	s := &Mux{
		Logger:    log.New(os.Stdout, "rst: ", log.LstdFlags),
		header:    make(http.Header),
		m:         gorillaMux.NewRouter(),
		endpoints: make(map[string]mapEndpoint),
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
	defer func() {
		if err := recover(); err != nil {
			reason := fmt.Sprintf("%s", err) // Stringer interface
			if !s.Debug {
				t := InternalServerError(reason, "", true)
				s.Logger.Println(t.String())
				reason = http.StatusText(http.StatusInternalServerError)
			}
			InternalServerError(reason, "", s.Debug).ServeHTTP(w, r)
		}
	}()

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

	setVars(r, RouteVars(match.Vars))
	defer delVars(r)

	if s.ac != nil {
		if handler, valid := match.Handler.(*endpointHandler); valid {
			newAccessControlHandler(handler.endpoint, s.ac).ServeHTTP(w, r)
		} else {
			newAccessControlHandler(nil, s.ac).ServeHTTP(w, r)
		}
	}
	match.Handler.ServeHTTP(newResponseWriter(w), r)
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

// Handle registers the handler function for the given pattern.
func (s *Mux) handleMethod(pattern string, method string, handler http.Handler) {
	if _, ok := s.endpoints[pattern]; !ok {
		s.endpoints[pattern] = make(mapEndpoint)
		s.m.Handle(pattern, EndpointHandler(s.endpoints[pattern]))
	}
	s.endpoints[pattern][method] = handler
}

// Get registers handler for GET requests on the given pattern.
func (s *Mux) Get(pattern string, handler GetFunc) {
	s.handleMethod(pattern, Get, handler)
}

// Post registers handler for POST requests on the given pattern.
func (s *Mux) Post(pattern string, handler PostFunc) {
	s.handleMethod(pattern, Post, handler)
}

// Put registers handler for PUT requests on the given pattern.
func (s *Mux) Put(pattern string, handler PutFunc) {
	s.handleMethod(pattern, Put, handler)
}

// Patch registers handler for PATCH requests on the given pattern.
func (s *Mux) Patch(pattern string, handler PatchFunc) {
	s.handleMethod(pattern, Put, handler)
}

// Delete registers handler for DELETE requests on the given pattern.
func (s *Mux) Delete(pattern string, handler DeleteFunc) {
	s.handleMethod(pattern, Delete, handler)
}

// match returns the route
func (s *Mux) match(r *http.Request) *gorillaMux.RouteMatch {
	var match gorillaMux.RouteMatch
	if !s.m.Match(r, &match) {
		return nil
	}
	return &match
}

// mapEndpoint defines HTTP handlers for a given set of
type mapEndpoint map[string]http.Handler

// allowedMethods returns an array containing the HTTP methods supported by
// this endpoint.
func (e mapEndpoint) allowedMethods() []string {
	var methods []string
	for method := range e {
		methods = append(methods, method)
	}
	if _, ok := e[Get]; ok {
		methods = append(methods, Head)
	}
	return methods
}

// validateMethod returns an error if the method of r is not allowed by this
// endpoint.
func (e mapEndpoint) validateMethod(r *http.Request) error {
	if _, ok := e[r.Method]; !ok {
		return MethodNotAllowed(r.Method, e.allowedMethods())
	}
	return nil
}

// Get implements the Getter interface.
func (e mapEndpoint) Get(vars RouteVars, r *http.Request) (Resource, error) {
	if err := e.validateMethod(r); err != nil {
		return nil, err
	}
	fn := e[r.Method].(GetFunc)
	return fn(vars, r)
}

// Post implements the Poster interface.
func (e mapEndpoint) Post(vars RouteVars, r *http.Request) (Resource, string, error) {
	if err := e.validateMethod(r); err != nil {
		return nil, "", err
	}
	fn := e[r.Method].(PostFunc)
	return fn(vars, r)
}

// Put implements the Putter interface.
func (e mapEndpoint) Put(vars RouteVars, r *http.Request) (Resource, error) {
	if err := e.validateMethod(r); err != nil {
		return nil, err
	}
	fn := e[r.Method].(PutFunc)
	return fn(vars, r)
}

// Patch implements the Patcher interface.
func (e mapEndpoint) Patch(vars RouteVars, r *http.Request) (Resource, error) {
	if err := e.validateMethod(r); err != nil {
		return nil, err
	}
	fn := e[r.Method].(PatchFunc)
	return fn(vars, r)
}

// Delete implements the Deleter interface.
func (e mapEndpoint) Delete(vars RouteVars, r *http.Request) error {
	if err := e.validateMethod(r); err != nil {
		return nil
	}
	fn := e[r.Method].(DeleteFunc)
	return fn(vars, r)
}

// Envelope is a wrapper to allow any interface{} to be used as an rst.Resource
// interface.
type Envelope struct {
	projection   interface{}
	lastModified time.Time
	etag         string
	ttl          time.Duration
	header       http.Header
}

// Header returns the list of headers that will be added to the ResponseWriter.
func (e *Envelope) Header() http.Header {
	return e.header
}

// Projection of the resource wrapped in this envelope.
func (e *Envelope) Projection() interface{} {
	return e.projection
}

// TTL implements the rst.Resource interface.
func (e *Envelope) TTL() time.Duration {
	return e.ttl
}

// LastModified implements the rst.Resource interface.
func (e *Envelope) LastModified() time.Time {
	return e.lastModified
}

// ETag implements the rst.Resource interface.
func (e *Envelope) ETag() string {
	return e.etag
}

// MarshalRST marshals projection.
func (e *Envelope) MarshalRST(r *http.Request) (string, []byte, error) {
	return Marshal(e.projection, r)
}

// ServeHTTP implements http.Handler. e.MarshalRST will be called internally.
func (e *Envelope) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	contentType, b, err := e.MarshalRST(r)
	if err != nil {
		writeError(err, w, r)
		return
	}

	w.Header().Set("Content-Type", contentType)
	if e.header != nil {
		for key, values := range e.header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
	}

	if compression := getCompressionFormat(b, r); compression != "" {
		w.Header().Set("Content-Encoding", compression)
	}

	if strings.ToUpper(r.Method) == Post {
		w.WriteHeader(http.StatusCreated)
		w.Write(b)
		return
	}

	if len(b) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if w.Header().Get("Content-Range") != "" {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if strings.ToUpper(r.Method) == Head {
		return
	}
	w.Write(b)
}

// NewEnvelope returns a struct that marshals projection when used as an
// rst.Resource interface.
func NewEnvelope(projection interface{}, lastModified time.Time, etag string, ttl time.Duration) *Envelope {
	return &Envelope{
		projection:   projection,
		lastModified: lastModified,
		etag:         etag,
		ttl:          ttl,
		header:       make(http.Header),
	}
}
