package rst

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"testing"
)

func TestAddVary(t *testing.T) {
	var compareFn = func(h, o http.Header) {
		bh, bo := new(bytes.Buffer), new(bytes.Buffer)
		h.Write(bh)
		o.Write(bo)

		if bh.String() != bo.String() {
			t.Fatal("addVary did not return the expected result.")
		}
	}

	h := make(http.Header)
	addVary(h, "Accept")
	addVary(h, "Range")
	addVary(h, "Accept")
	addVary(h, "Accept-Encoding")

	o := make(http.Header)
	addVary(o, "Accept")
	addVary(o, "Range")
	addVary(o, "Accept-Encoding")

	compareFn(h, o)
}

func TestParseRange(t *testing.T) {
	var test = func(raw, unit string, from, to uint64) {
		parsed, err := ParseRange(raw)
		if err != nil {
			t.Errorf("%s: %s", raw, err)
			return
		}

		if parsed.Unit != unit {
			t.Errorf("%s: expected Unit %s. Got %s", raw, unit, parsed.Unit)
		}
		if parsed.From != from {
			t.Errorf("%s: expected From %d. Got %d", raw, from, parsed.From)
		}
		if parsed.To != to {
			t.Errorf("%s: expected To %d. Got %s", raw, to, parsed.Unit)
		}
	}
	test("bytes=12-100", "bytes", 12, 100)
	test("something=0-932", "something", 0, 932)
	test("resources=35-", "resources", 35, math.MaxUint64)
	test("blablabla=1938272910-3438272910", "blablabla", 1938272910, 3438272910)

	if _, err := ParseRange("bytes 12-234"); err == nil {
		t.Errorf("Error not cached")
	}

	if _, err := ParseRange("bytes=12-10"); err == nil {
		t.Errorf("Error not cached")
	}
}

func TestAcceptAdjust(t *testing.T) {
	from, to := uint64(15), uint64(100000)
	rg := &Range{"resources", from, to}
	rg.adjust(testPeopleResourceCollection)

	if from != rg.From {
		t.Fatal("from did not match. Got:", rg.From, "Wanted:", from)
	}

	if rg.To != testPeopleResourceCollection.Count()-1 {
		t.Fatal("to did not match. Got:", rg.To, "Wanted:", testPeopleResourceCollection.Count()-1)
	}
}

func TestParseAccept(t *testing.T) {
	chrome := ParseAccept("image/png,*/*;q=0.5,text/plain;q=0.8,application/xml,application/xhtml+xml,text/html;q=0.9")
	if expected := 6; len(chrome) != expected {
		t.Errorf("expected %d. Got %d", expected, len(chrome))
	}

	expected := []string{
		"image/png",
		"application/xml",
		"application/xhtml+xml",
		"text/html",
		"text/plain",
		"*/*",
	}
	for i, item := range chrome {
		if s := fmt.Sprintf("%s/%s", item.Type, item.SubType); s != expected[i] {
			t.Errorf("expected %s at index %d, got %s", expected[i], i, s)
		}
	}
}

func TestAcceptNegociate(t *testing.T) {
	chrome := ParseAccept("application/xml,application/xhtml+xml,text/html;q=0.9,text/plain;q=0.8,image/png,*/*;q=0.5")
	var test = func(alternatives []string, expected string) {
		if ct := chrome.Negotiate(alternatives...); ct != expected {
			t.Errorf("got %s expected %s", ct, expected)
		}
	}

	test([]string{"text/html", "image/png"}, "image/png")
	test([]string{"text/html", "text/plain", "text/n3"}, "text/html")
	test([]string{"text/n3", "text/plain"}, "text/plain")
	test([]string{"text/n3", "application/rdf+xml"}, "text/n3")
}
