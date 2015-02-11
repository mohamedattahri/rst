package rst

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"mime"
	"net/http"
	"testing"
)

// Checking if marshalXML inserts a header and outputs a valid xml document
// when used with a struct.
func TestMarshalXMLStruct(t *testing.T) {
	type Group struct {
		People  []*person `xml:"person"`
		XMLName xml.Name  `xml:"group"`
	}
	input := &Group{People: testPeople[:5]}

	b, err := marshalXML(input)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b[:len(xml.Header)], []byte(xml.Header)) {
		t.Fatal("XML marshaling did not start with a proper header")
	}

	var parsed Group
	decoder := xml.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if len(parsed.People) != len(input.People) {
		t.Fatal("len error. Got:", len(parsed.People), "Wanted:", len(input.People))
	}

	for i, c := range parsed.People {
		if c.String() != input.People[i].String() {
			t.Fatal("Got:", c.String(), "Wanted:", input.People[i].String())
		}
	}
}

// Checking that marshalXML inserts the header and the envelope around arrays
// marshaled with xml.MarshalResource
func TestMarshalXMLArray(t *testing.T) {
	people := testPeople[:10]

	b, err := marshalXML(people)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(b[:len(xml.Header)], []byte(xml.Header)) {
		t.Fatal("XML marshaling did not start with a proper header")
	}

	var peopleList struct {
		People []*person `xml:"person"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(b))
	if err := decoder.Decode(&peopleList); err != nil {
		t.Fatal(err)
	}
	if len(peopleList.People) != len(people) {
		t.Fatal("len error. Got:", len(peopleList.People), "Wanted:", len(people))
	}

	for i, c := range peopleList.People {
		if c.String() != people[i].String() {
			t.Fatal("Got:", c.String(), "Wanted:", people[i].String())
		}
	}
}

// Testing whether Marshal negotiates content types correctly.
func TestMarshal(t *testing.T) {
	var generate = func(contentType string) *http.Request {
		r, _ := newRequest(
			fmt.Sprintf(
				"GET /index.html HTTP/1.1\nHost: www.example.com\nAccept: %s\n\n",
				contentType,
			),
		)
		return r
	}

	var test = func(contentType, expecting string) {
		ct, _, err := MarshalResource(testPeople[0], generate(contentType))
		if err != nil {
			t.Fatal(err)
		}

		mime, _, err := mime.ParseMediaType(ct)
		if err != nil {
			t.Errorf("unable to parse mime %s: %s", ct, err)
		}

		if mime != expecting {
			t.Errorf("expecting %s. Got %s", expecting, mime)
		}
	}

	// Supported formats
	test("application/json", "application/json")
	test("text/javascript", "application/json")
	test("application/xml", "application/xml")
	test("text/xml", "application/xml")
	test("text/plain", "text/plain")
	test("*/*", "application/json")
	test("", "application/json")
	test("image/png,*/*;q=0.5,text/plain;q=0.8,application/xml,application/xhtml+xml,text/html;q=0.9", "application/xml")

	// Errors
	_, _, err := MarshalResource(testPeople[0], generate("image/png"))
	if err == nil {
		t.Fatal(err)
	}
	if e, valid := err.(*Error); !valid || e.Code != http.StatusNotAcceptable {
		t.Errorf("Expecting error with code %d. Got: %s", http.StatusNotAcceptable, err)
	}
}

// Testing whether marshalResource handles the Marshaler interface correctly.
type customPerson person

func (c *customPerson) MarshalRST(r *http.Request) (string, []byte, error) {
	return "text/plain", []byte("hello, world!"), nil
}
func TestMarshalResource(t *testing.T) {
	c := &customPerson{}
	ct, b, err := Marshal(c, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ct != "text/plain" {
		t.Fatal("Got:", ct, "Wanted: text/plain")
	}
	if string(b) != "hello, world!" {
		t.Fatal("Got:", string(b), "Wanted: hello, world!")
	}
}
