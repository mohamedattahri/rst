package rst

import (
	"bytes"
	"encoding"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

var alternatives = []string{
	"application/json",
	"text/javascript",
	"application/xml",
	"text/xml",
	"text/plain",
	"*/*",
}

/*
Marshaler is implemented by resources wishing to handle their encoding
on their own.

Example:
	const png = "image/png"

	type User struct{}
	// assuming User implements rst.Resource

	// MarshalRST returns the profile picture of the user if the Accept header
	// of the request indicates "image/png", and relies on rst.MarshalResource
	// to handle the other cases.
	func (u *User) MarshalRST(r *http.Request) (string, []byte, error) {
		accept := ParseAccept(r.Header.Get("Accept"))
		if accept.Negotiate(png) == png {
			b, err := ioutil.ReadFile("path/of/user/profile/picture.png")
			return png, b, err
		}
		return rst.MarshalResource(u, r)
	}
*/
type Marshaler interface {
	// MarshalRST must return the chosen encoding media MIME type and the
	// encoded resource as an array of bytes, or an error.
	//
	// MarshalRST is to rst.Marshal what MarshalJSON is to json.Marshal.
	MarshalRST(*http.Request) (contentType string, data []byte, err error)
}

var jsonNull = []byte("null")

// MarshalResource negotiates contentType based on the Accept header in r, and returns
// the encoded version of resource as an array of bytes.
//
// MarshalResource can encode a resource in JSON and XML, as well as text using either
// encoding.TextMarshaler or fmt.Stringer.
//
// MarshalResource's XML marshaling will always return a valid XML document with a
// header and a root object, which is not the case for the encoding/xml package.
//
// MarshalResource can be called from Marshaler.MarshalRST on the same resource safely.
func MarshalResource(resource interface{}, r *http.Request) (contentType string, encoded []byte, err error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if len(accept) == 0 {
		accept = append(accept, AcceptClause{
			Type:    "*",
			SubType: "*",
			Params:  make(map[string]string),
			Q:       1.0,
		})
	}

	switch accept.Negotiate(alternatives...) {
	case "application/json", "text/javascript":
		b, err := json.Marshal(resource)
		if bytes.Equal(b, jsonNull) {
			b = []byte{}
		}
		return "application/json; charset=utf-8", b, err
	case "application/xml", "text/xml":
		b, err := marshalXML(resource)
		return "application/xml; charset=utf-8", b, err
	case "text/plain":
		if marshaler, implemented := resource.(encoding.TextMarshaler); implemented {
			b, err := marshaler.MarshalText()
			return "text/plain; charset=utf-8", b, err
		}
		if marshaler, implemented := resource.(fmt.Stringer); implemented {
			return "text/plain; charset=utf-8", []byte(marshaler.String()), nil
		}
	}
	return "", nil, NotAcceptable()
}

// marshalXML adds an XML header and an envelope when needed to the result
// obtained from calling xml.Marshal on resource.
func marshalXML(resource interface{}) ([]byte, error) {
	b, err := xml.Marshal(resource)
	if err != nil {
		return nil, err
	}

	if len(b) >= len(xml.Header) && bytes.Equal(b[:len(xml.Header)], []byte(xml.Header)) {
		return b, err
	}

	// Arrays and slices need an envelope.
	if t := reflect.TypeOf(resource); t.Kind() == reflect.Array || t.Kind() == reflect.Slice {
		fqn := strings.Split(t.String(), ".")
		name := fqn[len(fqn)-1] + "List"
		prefix := []byte("<" + name + ">")
		suffix := []byte("</" + name + ">")
		b = bytes.Join([][]byte{prefix, b, suffix}, nil)
	}

	// Adding XML header
	b = bytes.Join([][]byte{[]byte(xml.Header), b}, nil)

	return b, err
}

// Marshal negotiates contentType based on the Accept header in r, and returns
// the encoded version of resource as an array of bytes.
//
// Marshal uses resource.MarshalRST if resource implements the Marshaler
// interface, or MarshalResource method if it doesn't.
func Marshal(resource interface{}, r *http.Request) (contentType string, encoded []byte, err error) {
	if marshaler, implemented := resource.(Marshaler); implemented {
		return marshaler.MarshalRST(r)
	}

	return MarshalResource(resource, r)
}
