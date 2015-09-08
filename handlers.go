package rst

import (
	"net/http"
	"strings"
	"time"
)

// noContent is used to run ResponseWriter.Write even when there's not data to
// write.
var noContent = []byte{}

/*
Resource represents a resource exposed on a REST service using an Endpoint.

There are other interfaces that can be implemented by a resource to either
control its projection in a response payload, or add support for advanced HTTP
features:

- The Ranger interface adds support for range requests and allows the resource to
return partial responses.

- The Marshaler interface allows you to customize the encoding process of the
resource and control the bytes returned in the payload of the response.

- The http.Handler interface can be used to gain direct access to the
ResponseWriter and Request. This is a low level method that should only be used
when you need to write chunked responses, or if you wish to add specific headers
such a Content-Disposition, etc.
*/
type Resource interface {
	ETag() string            // ETag identifying the current version of the resource.
	LastModified() time.Time // Date and time of the last modification of the resource.
	TTL() time.Duration      // Time to live, or caching duration of the resource.
}

/*
ValidateConditions returns true if the If-Unmodified-Since or the If-Match headers of
r are not matching with the current version of resource.

	func (ep *endpoint) Patch(vars RouteVars, r *http.Request) (Resource, error) {
		resource := db.Lookup(vars.Get("id"))
		if ValidateConditions(resource, r) {
			return nil, Conflict()
		}

		// apply the patch safely from here
	}
*/
func ValidateConditions(resource Resource, r *http.Request) bool {
	if d, err := time.Parse(rfc1123, r.Header.Get("If-Unmodified-Since")); err == nil {
		if d.Sub(resource.LastModified()) < 0 {
			return true
		}
	}
	if etag := r.Header.Get("If-Match"); etag != "" && etag != resource.ETag() {
		return true
	}
	return false
}

/*
Ranger is implemented by resources that support partial responses.

Range will only be called if the request contains a valid Range header.
Otherwise, it will be processed as a normal Get request.

	type Doc []byte
	// assuming Doc implements rst.Resource interface

	// Supported units will be displayed in the Accept-Range header
		func (d *Doc) Units() []string {
		return []string{"bytes"}
	}

	// Count returns the total number of range units available
	func (d *Doc) Count() uint64 {
		return uint64(len(d))
	}

	func (d *Doc) Range(rg *rst.Range) (*rst.ContentRange, rst.Resource, error) {
		cr := &ContentRange{rg, c.Count()}
		part := d[rg.From : rg.To+1]
		return cr, part, nil
	}
*/
type Ranger interface {
	// Supported range units
	Units() []string

	// Total number of units available
	Count() uint64

	// Range is used to return the part of the resource that is indicated by the
	// passed range.
	Range(*Range) (*ContentRange, Resource, error)
}

func writeError(err error, w http.ResponseWriter, r *http.Request) {
	ErrorHandler(err).ServeHTTP(w, r)
}

func writeResource(resource Resource, w http.ResponseWriter, r *http.Request) {
	// Time-based conditional retrieval
	if t, err := time.Parse(rfc1123, r.Header.Get("If-Modified-Since")); err == nil {
		if t.Sub(resource.LastModified()).Seconds() >= 0 {
			w.WriteHeader(http.StatusNotModified)
			w.Write(noContent)
			return
		}
	}

	// ETag-based conditional retrieval
	for _, t := range strings.Split(r.Header.Get("If-None-Match"), ";") {
		if t == resource.ETag() {
			w.WriteHeader(http.StatusNotModified)
			w.Write(noContent)
			return
		}
	}

	// Headers
	addVary(w.Header(), "Accept")
	w.Header().Set("Last-Modified", resource.LastModified().UTC().Format(rfc1123))
	w.Header().Set("ETag", resource.ETag())
	w.Header().Set("Expires", time.Now().Add(resource.TTL()).UTC().Format(rfc1123))

	// If resource implements http.Handler, let it write in the ResponseWriter
	// on its own.
	if handler, implemented := resource.(http.Handler); implemented {
		handler.ServeHTTP(w, r)
		return
	}

	var (
		contentType string
		b           []byte
		err         error
	)
	contentType, b, err = Marshal(resource, r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)

	if compression := getCompressionFormat(b, r); compression != "" {
		w.Header().Set("Content-Encoding", compression)
		addVary(w.Header(), "Accept-Encoding")
	}

	if strings.ToUpper(r.Method) == Post {
		w.WriteHeader(http.StatusCreated)
		w.Write(b)
		return
	}

	if len(b) == 0 {
		w.WriteHeader(http.StatusNoContent)
		w.Write(noContent) // Allowing an override of ResponseWriter to work.
		return
	}

	if w.Header().Get("Content-Range") != "" {
		w.WriteHeader(http.StatusPartialContent)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if strings.ToUpper(r.Method) == Head {
		w.Write(noContent)
		return
	}
	w.Write(b)
}

/*
Endpoint represents an access point exposing a resource in the REST service.
*/
type Endpoint interface{}

/*
Getter is implemented by endpoints allowing the GET and HEAD method.

	func (ep *endpoint) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}
		return resource, nil
	}
*/
type Getter interface {
	// Returns the resource or an error. A nil resource pointer will generate
	// a response with status code 204 No Content.
	Get(RouteVars, *http.Request) (Resource, error)
}

// GetFunc allows a Getter.Get method to be used an http.Handler.
type GetFunc func(RouteVars, *http.Request) (Resource, error)

// ServeHTTP implements the http.Handler interface.
func (f GetFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	vars := getVars(r)

	resource, err := f(vars, r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	if resource == nil {
		w.WriteHeader(http.StatusNoContent)
		w.Write(noContent)
		return
	}

	// Check if resource implements Ranger
	ranger, implemented := resource.(Ranger)
	if !implemented {
		writeResource(resource, w, r)
		return
	}
	w.Header().Set("Accept-Ranges", strings.Join(ranger.Units(), ", "))

	// Check if request contains a valid Range header, and check whether it's
	// a valid range.
	rg, err := ParseRange(r.Header.Get("Range"))
	if err != nil || rg.validate(ranger) != nil {
		writeResource(resource, w, r)
		return
	}

	// If-Range can either contain an ETag, or a date.
	// If the precondition fails, the Range header is ignored and the full
	// resource is returned.
	if raw := r.Header.Get("If-Range"); raw != "" {
		date, _ := time.Parse(rfc1123, raw)
		if !date.Equal(resource.LastModified()) && raw != resource.ETag() {
			writeResource(resource, w, r)
			return
		}
	}

	if err := rg.adjust(ranger); err != nil {
		writeError(err, w, r)
		return
	}

	cr, partial, err := ranger.Range(rg)
	if err != nil {
		writeError(err, w, r)
		return
	}

	addVary(w.Header(), "Range")
	w.Header().Set("Content-Range", cr.String())
	writeResource(partial, w, r)
}

/*
Patcher is implemented by endpoints allowing the PATCH method.

	func (ep *endpoint) Patch(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}

		if r.Header.Get("Content-Type") != "application/www-form-urlencoded" {
			return nil, rst.UnsupportedMediaType("application/www-form-urlencoded")
		}

		// Detect any writing conflicts
		if rst.ValidateConditions(resource, r) {
			return nil, rst.PreconditionFailed()
		}

		// Read r.Body, apply changes to resource, then return it
		return resource, nil
	}
*/
type Patcher interface {
	// Returns the patched resource or an error.
	Patch(RouteVars, *http.Request) (Resource, error)
}

// PatchFunc allows a Patcher.Patch method to be used an http.Handler.
type PatchFunc func(RouteVars, *http.Request) (Resource, error)

// ServeHTTP implements the http.Handler interface.
func (f PatchFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	if resource == nil {
		w.Write(noContent)
		return
	}
	writeResource(resource, w, r)
}

/*
Putter is implemented by endpoints allowing the PUT method.

	func (ep *endpoint) Put(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource == nil {
			return nil, rst.NotFound()
		}

		// Detect any writing conflicts
		if rst.ValidateConditions(resource, r) {
			return nil, rst.PreconditionFailed()
		}

		// Read r.Body, apply changes to resource, then return it
		return resource, nil
	}
*/
type Putter interface {
	// Returns the modified resource or an error.
	Put(RouteVars, *http.Request) (Resource, error)
}

// PutFunc allows a Putter.Put method to be used an http.Handler.
type PutFunc func(RouteVars, *http.Request) (Resource, error)

// ServeHTTP implements the http.Handler interface.
func (f PutFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	if resource == nil {
		w.Write(noContent)
		return
	}
	writeResource(resource, w, r)
}

/*
Poster is implemented by endpoints allowing the POST method.

	func (ep *endpoint) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, string, error) {
		resource, err := NewResourceFromRequest(r)
		if err != nil {
			return nil, "", err
		}
		uri := "https://example.com/resource/" + resource.ID
		return resource, uri, nil
	}
*/
type Poster interface {
	// Returns the resource newly created and the URI where it can be located, or
	// an error.
	Post(RouteVars, *http.Request) (resource Resource, location string, err error)
}

// PostFunc allows a Poster.Post method to be used an http.Handler.
type PostFunc func(RouteVars, *http.Request) (Resource, string, error)

// ServeHTTP implements the http.Handler interface.
func (f PostFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, location, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}

	if location != "" {
		// TODO: make sure the URI is a fully qualified URL
		w.Header().Set("Location", location)
	}

	if resource == nil {
		w.WriteHeader(http.StatusCreated)
		w.Write(noContent)
		return
	}
	writeResource(resource, w, r)
}

// Deleter is implemented by endpoints allowing the DELETE method.
type Deleter interface {
	Delete(RouteVars, *http.Request) error
}

// DeleteFunc allows a Deleter.Deleter method to be used an http.Handler.
type DeleteFunc func(RouteVars, *http.Request) error

// ServeHTTP implements the http.Handler interface.
func (f DeleteFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := f(getVars(r), r); err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
	w.Write(noContent)
}

// OptionsHandler returns a handler that serves responses to OPTIONS requests
// issued to the resource exposed by the given endpoint.
func optionsHandler(endpoint Endpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer w.Write(noContent)
		if r.Method != Options {
			return
		}

		w.Header().Set("Allow", strings.Join(AllowedMethods(endpoint), ", "))
		w.Header().Set("Content-Type", strings.Join(alternatives, ";"))
		w.WriteHeader(http.StatusNoContent)
	})
}

// EndpointHandler returns a handler that serves HTTP requests for the resource
// exposed by the given endpoint.
func EndpointHandler(endpoint Endpoint) http.Handler {
	return &endpointHandler{endpoint}
}

type endpointHandler struct {
	endpoint Endpoint
}

func (h *endpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	methodHandler := getMethodHandler(h.endpoint, r.Method, r.Header)
	if methodHandler == nil {
		if allowed := AllowedMethods(h.endpoint); len(allowed) > 0 {
			methodHandler = MethodNotAllowed(r.Method, allowed)
		} else {
			methodHandler = NotFound()
		}
	}
	methodHandler.ServeHTTP(w, r)
}

// getMethodHandler returns the handler in endpoint for the given of HTTP
// request method and header
func getMethodHandler(endpoint Endpoint, method string, header http.Header) http.Handler {
	switch strings.ToUpper(method) {
	case Options:
		return optionsHandler(endpoint)
	case Head, Get:
		if i, supported := endpoint.(Getter); supported {
			return GetFunc(i.Get)
		}
	case Patch:
		if i, supported := endpoint.(Patcher); supported {
			return PatchFunc(i.Patch)
		}
	case Put:
		if i, supported := endpoint.(Putter); supported {
			return PutFunc(i.Put)
		}
	case Post:
		if i, supported := endpoint.(Poster); supported {
			return PostFunc(i.Post)
		}
	case Delete:
		if i, supported := endpoint.(Deleter); supported {
			return DeleteFunc(i.Delete)
		}
	}
	return nil
}

var supportedMethods = []string{Head, Get, Patch, Put, Post, Delete}

// methodLister is implements by endpoints that need to control the list of
// HTTP methods they support.
type methodLister interface {
	allowedMethods() []string
}

// AllowedMethods returns the list of HTTP methods allowed by this endpoint.
func AllowedMethods(endpoint Endpoint) (methods []string) {
	if lister, ok := endpoint.(methodLister); ok {
		return lister.allowedMethods()
	}

	for _, method := range supportedMethods {
		if getMethodHandler(endpoint, method, nil) != nil {
			methods = append(methods, method)
		}
	}
	return methods
}
