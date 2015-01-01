package rst

import (
	"fmt"
	"net/http"
	"strings"
)

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
// no support by the server. The supported media MIME type strings can be passed
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
	if cr.Total == 0 {
		err.Header.Set("Content-Range", "*/*")
	} else {
		err.Header.Set("Content-Range", fmt.Sprintf("%s */%d", cr.Unit, cr.Total))
	}
	err.Header.Add("Vary", "Range")
	return err
}

// Error represents an HTTP error, with a status code, a reason and a
// description.
// Error is both a valid Go error and a client of the http.Handler interface.
//
// Header can be used to specify headers that will be written in the HTTP
// response generated from this error.
type Error struct {
	Code        int         `json:"-" xml:"-"`
	Header      http.Header `json:"-" xml:"-"`
	Reason      string      `json:"message" xml:"Message"`
	Description string      `json:"description" xml:"Description"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d (%s) - %s\n%s", e.Code, http.StatusText(e.Code), e.Reason, e.Description)
}

func (e *Error) String() string {
	return e.Error()
}

// MarshalREST is implemented to generate an HTML rendering of the error.
func (e *Error) MarshalREST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	ct := accept.Negotiate("text/html", "*/*")
	if strings.Contains(ct, "html") || ct == "*/*" {
		html := fmt.Sprintf(
			`<!DOCTYPE html><html><head><title>%d (%s)</title></head><body><h1>%s</h1><p>%s</p></body></html>`,
			e.Code,
			http.StatusText(e.Code),
			e.Reason,
			e.Description,
		)
		return "text/html", []byte(html), nil
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

	w.Header().Set("Content-Type", ct)
	w.Header().Add("Vary", "Accept")
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
