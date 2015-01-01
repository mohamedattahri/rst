package rst

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

/*
ErrUnsupportedRangeUnit is used to ignore a Range header when handling
a Get request, because the range unit requested is unsupported.
	type collection []Resource

	func (c collection) Range(rg *Range) (*ContentType, Resource, error) {
		if rg.Unit == "unsupported" {
			// Returns a response in which c is entirely present
			return nil, nil, ErrUnsupportedRangeUnit
		}

		if someCondition(c) == false {
			// Returns an HTTP response with status code 400.
			return nil, nil, BadRequest("someCondition has failed.", "Do something different")
		}

		return &ContentRange{rg, c.Count()}, collection[rg.From, rg.To+1], nil
	}
*/
var ErrUnsupportedRangeUnit = errors.New("unsupported range unit")

// Resource represents a resource exposed on a REST service using an Endpoint.
type Resource interface {
	ETag() string            // ETag identifying the current version of the resource.
	LastModified() time.Time // Date and time of the last modification of the resource.
	TTL() time.Duration      // Time to live, or caching duration of the resource.
}

/*
Conflicts returns true if the If-Unmodified-Since or the If-Match headers of
r are conflicting with the current version of resource.

	func (ep *endpoint) Patch(vars RouteVars, r *http.Request) (Resource, error) {
		resource := db.Lookup(vars.Get("id"))
		if Conflicts(resource, r) {
			return nil, PreconditionFailed()
		}

		// apply the patch safely from here
	}

*/
func Conflicts(resource Resource, r *http.Request) bool {
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

	// Count returns the total number of range units available
	func (d *Doc) Count() uint64 {
		return uint64(len(d))
	}

	func (d *Doc) Range(rg *rst.Range) (*rst.ContentRange, rst.Resource, error) {
		if rg.Unit != "bytes" {
			// the Range header is ignored if the range unit passed is not bytes.
			// Request will be processed like a normal HTTP Get request because
			// ErrUnsupportedRangeUnit is returned.
			return nil, nil, ErrUnsupportedRangeUnit
		}
		cr := &ContentRange{rg, c.Count()}
		part := d[rg.From : rg.To+1]
		return cr, part, nil
	}
*/
type Ranger interface {
	// Total number of units available
	Count() uint64

	// Range is used to return the part of the resource that is indicated by the
	// passed range.
	//
	// If the error is ErrUnsupportedRangeUnit, the attempt to handle
	// the request as a partial GET will be canceled and the entire resource will
	// be returned.
	Range(*Range) (*ContentRange, Resource, error)
}

func writeError(e error, w http.ResponseWriter, r *http.Request) {
	if err, valid := e.(*Error); valid {
		err.ServeHTTP(w, r)
		return
	}
	err := NewError(
		http.StatusInternalServerError,
		http.StatusText(http.StatusInternalServerError),
		e.Error(),
	)
	err.ServeHTTP(w, r)
}

func writeResource(resource Resource, w http.ResponseWriter, r *http.Request) {
	// Time-based conditional retrieval
	if t, err := time.Parse(rfc1123, r.Header.Get("If-Modified-Since")); err == nil {
		if t.Sub(resource.LastModified()).Seconds() >= 0 {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	// ETag-based conditional retrieval
	for _, t := range strings.Split(r.Header.Get("If-None-Match"), ";") {
		if t == resource.ETag() {
			w.WriteHeader(http.StatusNotModified)
			return
		}
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
	w.Header().Add("Vary", "Accept")

	// Headers
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Last-Modified", resource.LastModified().UTC().Format(rfc1123))
	w.Header().Set("Expires", time.Now().Add(resource.TTL()).UTC().Format(rfc1123))

	if compression := getCompressionFormat(b, r); compression != "" {
		w.Header().Set("Content-Encoding", compression)
		w.Header().Add("Vary", "Accept-Encoding")
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

// Endpoint represents an access point exposing a resource in the REST service.
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

// getFunc is an adapter to use ordinary functions as HTTP Get handlers.
type getFunc func(RouteVars, *http.Request) (Resource, error)

func (f getFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	if resource == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Check if resource is a Ranger, and the request contains a valid Range
	// header
	rg, err := ParseRange(r.Header.Get("Range"))
	ranger, implemented := resource.(Ranger)
	if !implemented || err != nil {
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

	if err := rg.Adjust(ranger); err != nil {
		writeError(err, w, r)
		return
	}

	cr, partial, err := ranger.Range(rg)
	if err == ErrUnsupportedRangeUnit {
		writeResource(resource, w, r)
		return
	}
	if err != nil {
		writeError(err, w, r)
		return
	}

	w.Header().Add("Vary", "Range")
	if cr.From != 0 || cr.To != (cr.Total-1) {
		w.Header().Set("Content-Range", cr.String())
	}
	writeResource(partial, w, r)
}

/*
Patcher is implemented by endpoints allowing the PATCH method.

	func (ep *endpoint) Patch(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource != nil {
			return nil, rst.NotFound()
		}

		if r.Header.Get("Content-Type") != "application/www-form-urlencoded" {
			return nil, rst.UnsupportedMediaType("application/www-form-urlencoded")
		}

		// Detect any writing conflicts
		if rst.Conflicts(resource, r) {
			return nil, rst.PreconditionFailed()
		}

		// Read r.Body and an apply changes to resource
		// then return it
		return resource, nil
	}
*/
type Patcher interface {
	// Returns the patched resource or an error.
	Patch(RouteVars, *http.Request) (Resource, error)
}

// patchFunc is an adapter to use ordinary functions as HTTP PATCH handlers.
type patchFunc func(RouteVars, *http.Request) (Resource, error)

func (f patchFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	if resource == nil {
		return
	}
	writeResource(resource, w, r)
}

/*
Putter is implemented by endpoints allowing the PUT method.

	func (ep *endpoint) Put(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
		resource := database.Find(vars.Get("id"))
		if resource != nil {
			return nil, rst.NotFound()
		}

		// Detect any writing conflicts
		if rst.Conflicts(resource, r) {
			return nil, rst.PreconditionFailed()
		}

		// Read r.Body and an apply changes to resource
		// then return it
		return resource, nil
	}
*/
type Putter interface {
	// Returns the modified resource or an error.
	Put(RouteVars, *http.Request) (Resource, error)
}

// putFunc is an adapter to use ordinary functions as HTTP PUT handlers.
type putFunc func(RouteVars, *http.Request) (Resource, error)

func (f putFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusOK)
	if resource == nil {
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

// postFunc is an adapter to use ordinary functions as HTTP POST handlers.
type postFunc func(RouteVars, *http.Request) (Resource, string, error)

func (f postFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	resource, location, err := f(getVars(r), r)
	if err != nil {
		writeError(err, w, r)
		return
	}

	if location != "" {
		w.Header().Add("Location", location)
	}

	if resource == nil {
		// TODO: make sure the URI is a fully qualified URL

		w.WriteHeader(http.StatusCreated)
		return
	}
	writeResource(resource, w, r)
}

// Deleter is implemented by endpoints allowing the DELETE method.
type Deleter interface {
	Delete(RouteVars, *http.Request) error
}

// deleteFunc is an adapter to use ordinary functions as HTTP DELETE handlers.
type deleteFunc func(RouteVars, *http.Request) error

func (f deleteFunc) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := f(getVars(r), r); err != nil {
		writeError(err, w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// OptionsHandler returns a handler that serves responses to OPTIONS requests
// issued to the resource exposed by the given endpoint.
func optionsHandler(endpoint Endpoint) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			return getFunc(i.Get)
		}
	case Patch:
		if i, supported := endpoint.(Patcher); supported {
			return patchFunc(i.Patch)
		}
	case Put:
		if i, supported := endpoint.(Putter); supported {
			return putFunc(i.Put)
		}
	case Post:
		if i, supported := endpoint.(Poster); supported {
			return postFunc(i.Post)
		}
	case Delete:
		if i, supported := endpoint.(Deleter); supported {
			return deleteFunc(i.Delete)
		}
	}
	return nil
}

// AllowedMethods returns the list of HTTP methods allowed by this endpoint.
func AllowedMethods(endpoint Endpoint) (methods []string) {
	for _, method := range []string{Head, Get, Patch, Put, Post, Delete} {
		if getMethodHandler(endpoint, method, nil) != nil {
			methods = append(methods, method)
		}
	}
	return methods
}
