package rst

import (
	"bytes"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"runtime"
	"strings"

	"github.com/mohamedattahri/rst/internal/assets"
)

// ErrorHandler is a wrapper that allows any Go error to implement the
// http.Handler interface.
func ErrorHandler(err error) http.Handler {
	if e, ok := err.(*Error); ok {
		return e
	}
	// panic will be intercepted in the main mux handler, and will write a
	// response which may display debugging info or hide them depending on the
	// Debug variable set in the mux.
	panic(err)
}

// BadRequest is returned when the request could not be understood by the
// server due to malformed syntax.
func BadRequest(reason, description string) *Error {
	if reason == "" {
		reason = http.StatusText(http.StatusBadRequest)
	}
	if description == "" {
		description = "Request could not be understood due to malformed syntax."
	}

	return NewError(http.StatusBadRequest, reason, description)
}

// Unauthorized is returned when authentication is required for the server
// to process the request.
func Unauthorized() *Error {
	err := NewError(
		http.StatusUnauthorized,
		"Authentication is required",
		"Authentication is required and has failed or has not yet been provided.",
	)
	return err
}

// Forbidden is returned when a resource is protected and inaccessible.
func Forbidden() *Error {
	err := NewError(
		http.StatusForbidden,
		"Request will not be fullfilled",
		"The request was a valid request, but the server is refusing to respond to it. Authenticating will make no difference.",
	)
	return err
}

// NotFound is returned when the server has not found a resource matching the
// Request-URI.
func NotFound() *Error {
	return NewError(
		http.StatusNotFound,
		http.StatusText(http.StatusNotFound),
		"No resource could be found at the requested URI.",
	)
}

// MethodNotAllowed is returned when the method specified in a request is
// not allowed by the resource identified by the request-URI.
func MethodNotAllowed(forbidden string, allowed []string) *Error {
	methods := strings.Join(allowed, ", ")
	err := NewError(
		http.StatusMethodNotAllowed,
		fmt.Sprintf("%s method is not allowed for this resource", forbidden),
		fmt.Sprintf("This ressource only allows the following methods: %s.", methods),
	)
	err.Header.Set("Allow", methods)
	return err
}

// NotAcceptable is returned when the resource identified by the request
// is only capable of generating response entities which have content
// characteristics not acceptable according to the accept headers sent in the
// request.
func NotAcceptable() *Error {
	err := NewError(
		http.StatusNotAcceptable,
		http.StatusText(http.StatusNotAcceptable),
		"Resource is only capable of generating content not acceptable according to the accept headers sent in the request.",
	)
	return err
}

// Conflict is returned when a request can't be processed due to a conflict with
// the current state of the resource.
func Conflict() *Error {
	err := NewError(
		http.StatusConflict,
		"Resource could not be modified",
		"The request could not be processed due to a conflict with the current state of the resource.",
	)
	return err
}

// PreconditionFailed is returned when one of the conditions the request was
// made under has failed.
func PreconditionFailed() *Error {
	err := NewError(
		http.StatusPreconditionFailed,
		"Resource could not be modified",
		"A condition set in the headers of the request could not be matched.",
	)
	return err
}

// UnsupportedMediaType is returned when the entity in the request is in a format
// not support by the server. The supported media MIME type strings can be passed
// to improve the description of the error description.
func UnsupportedMediaType(mimes ...string) *Error {
	description := "The entity in the request is in a format not supported by this resource."
	if len(mimes) > 0 {
		description += fmt.Sprintf(" Supported types: %s", strings.Join(mimes, ", "))
	}
	err := NewError(
		http.StatusUnsupportedMediaType,
		"Entity inside request could not be processed",
		description,
	)
	return err
}

// RequestedRangeNotSatisfiable is returned when the range in the Range header
// does not overlap the current extent of the requested resource.
func RequestedRangeNotSatisfiable(cr *ContentRange) *Error {
	err := NewError(
		http.StatusRequestedRangeNotSatisfiable,
		http.StatusText(http.StatusRequestedRangeNotSatisfiable),
		"The requested range is not available and cannot be served.",
	)
	err.Header.Set("Content-Range", cr.String())
	addVary(err.Header, "Range")
	return err
}

type stackRecord struct {
	Filename string `json:"file" xml:"File"`
	Line     int    `json:"line" xml:"Line"`
	Funcname string `json:"func" xml:"Func"`
}

func (r *stackRecord) String() string {
	return fmt.Sprintf("Line %d: %s - %s", r.Line, r.Filename, r.Funcname)
}

// InternalServerError represents an error with status code 500.
//
// When captureStack is true, the stack trace will be captured and displayed in
// the HTML projection of the returned error if mux.Debug is true.
func InternalServerError(reason, description string, captureStack bool) *Error {
	err := NewError(http.StatusInternalServerError, reason, description)
	if captureStack {
		var stack []*stackRecord
		for skip := 2; ; skip++ {
			pc, file, line, ok := runtime.Caller(skip)
			if !ok {
				break
			}
			if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "runtime/panic.go") {
				continue
			}
			stack = append(stack, &stackRecord{
				Filename: file,
				Line:     line,
				Funcname: runtime.FuncForPC(pc).Name(),
			})
		}
		err.Stack = stack
	}
	return err
}

// Error represents an HTTP error, with a status code, a reason and a
// description.
// Error implements both the error and http.Handler interfaces.
//
// Header can be used to specify headers that will be written in the HTTP
// response generated from this error.
type Error struct {
	Code        int            `json:"-" xml:"-"`
	Header      http.Header    `json:"-" xml:"-"`
	Reason      string         `json:"message" xml:"Message"`
	Description string         `json:"description,omitempty" xml:"Description,omitempty"`
	Stack       []*stackRecord `json:"stack,omitempty" xml:"Stack,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d (%s) - %s\n%s", e.Code, http.StatusText(e.Code), e.Reason, e.Description)
}

func (e *Error) String() string {
	s := fmt.Sprintf("%d (%s) - %s", e.Code, http.StatusText(e.Code), e.Reason)

	if e.Description != "" {
		s += fmt.Sprintf("\n%s", e.Description)
	}

	if e.Stack != nil && len(e.Stack) > 0 {
		s += "\n"
		for _, r := range e.Stack {
			s += fmt.Sprintf("\n- %s", r)
		}
	}
	return s
}

// StatusText returns a text for the HTTP status code of this error. It returns
// the empty string if the code is unknown.
func (e *Error) StatusText() string {
	return http.StatusText(e.Code)
}

// MarshalRST is implemented to generate an HTML rendering of the error.
func (e *Error) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	ct := accept.Negotiate("text/html", "*/*")
	if strings.Contains(ct, "html") || ct == "*/*" {
		buffer := &bytes.Buffer{}
		var data = struct {
			Request *http.Request
			*Error
		}{Request: r, Error: e}
		if err := errorTemplate.Execute(buffer, &data); err != nil {
			return "", nil, err
		}
		return "text/html; charset=utf-8", buffer.Bytes(), nil
	}
	return MarshalResource(e, r)
}

// ServeHTTP implements the http.Handler interface.
func (e *Error) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ct, b, err := Marshal(e, r)
	if err != nil {
		ct = "text/plain; charset=utf-8"
		b = []byte(e.String())
	}

	for key, values := range e.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Remove headers which might have been set by a previous assumption of
	// success.
	w.Header().Del("Last-Modified")
	w.Header().Del("ETag")
	w.Header().Del("Expires")

	w.Header().Set("Content-Type", ct)
	addVary(w.Header(), "Accept")
	if e.Code != http.StatusNotFound && e.Code != http.StatusGone {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	}
	w.WriteHeader(e.Code)
	w.Write(b)
}

// NewError returns a new error with the given code, reason and description.
// It will panic if code < 400.
func NewError(code int, reason, description string) *Error {
	if code < 400 {
		panic(fmt.Errorf("%d is not a valid HTTP status code for an error", code))
	}
	return &Error{
		Code:        code,
		Reason:      reason,
		Description: description,
		Header:      make(http.Header),
	}
}

var errorTemplate *template.Template

func init() {
	// errorTemplate is based on data embedded in interal/assets/assets.go
	// using go generate and https://github.com/mjibson/esc.
	f, err := assets.FS(false).Open("/internal/assets/error.html")
	if err != nil {
		log.Fatal(err)
	}
	b, err := ioutil.ReadAll(f)
	if err != nil {
		log.Fatal(err)
	}
	errorTemplate, err = template.New("internal/assets/error.html").Parse(string(b))
	if err != nil {
		log.Fatal(err)
	}
}
