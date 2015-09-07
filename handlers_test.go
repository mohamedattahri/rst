package rst

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

var (
	tOptionsHandler = reflect.TypeOf(optionsHandler(nil))
	tGetFunc        = reflect.TypeOf(new(GetFunc)).Elem()
	tPostFunc       = reflect.TypeOf(new(PostFunc)).Elem()
	tPatchFunc      = reflect.TypeOf(new(PatchFunc)).Elem()
	tPutFunc        = reflect.TypeOf(new(PutFunc)).Elem()
	tDeleteFunc     = reflect.TypeOf(new(DeleteFunc)).Elem()
)

type allInterfaces struct{}

func (a *allInterfaces) Get(vars RouteVars, r *http.Request) (Resource, error) {
	panic("not implemented")
}
func (a *allInterfaces) Post(vars RouteVars, r *http.Request) (Resource, string, error) {
	panic("not implemented")
}
func (a *allInterfaces) Patch(vars RouteVars, r *http.Request) (Resource, error) {
	panic("not implemented")
}
func (a *allInterfaces) Put(vars RouteVars, r *http.Request) (Resource, error) {
	panic("not implemented")
}
func (a *allInterfaces) Delete(vars RouteVars, r *http.Request) error {
	panic("not implemented")
}

func TestValidateConditions(t *testing.T) {
	resource := testPeople[0]
	var test = func(d time.Time, etag string, expected bool) {
		header := make(http.Header)
		if !d.IsZero() {
			header.Set("If-Unmodified-Since", d.UTC().Format(rfc1123))
		}
		if etag != "" {
			header.Set("If-Match", etag)
		}
		rr := newRequestResponse(Post, testServerAddr+"/people", header, nil)
		if b := ValidateConditions(resource, rr.req); b != expected {
			t.Error("Conflicts. Wanted:", expected, "Got:", b)
		}
	}

	test(time.Time{}, "", false)                                           // nil, nil
	test(time.Time{}, resource.ETag(), false)                              // nil, false
	test(time.Time{}, "blabla", true)                                      // nil, true
	test(resource.LastModified(), "", false)                               // false, nil
	test(resource.LastModified().Add(24*time.Hour), "", false)             // false, nil
	test(resource.LastModified().Add(-24*time.Hour), "", true)             // true, nil
	test(resource.LastModified().Add(-4*time.Hour), resource.ETag(), true) // true, false
}

func TestAllowedMethods(t *testing.T) {
	supported := AllowedMethods(&allInterfaces{})
	expected := []string{Head, Get, Patch, Put, Post, Delete}
	for i, s := range supported {
		if s != expected[i] {
			t.Errorf("expected %s at index %d. Got %s", expected[i], i, s)
		}
	}
}

func TestResourceHTTPHandlerInterface(t *testing.T) {
	rr := newRequestResponse(Post, testServerAddr+"/chunked", nil, bytes.NewReader(testMBText))
	if err := rr.TestStatusCode(http.StatusOK); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHasHeader("Last-Modified"); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHasHeader("Etag"); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHasHeader("Expires"); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestBody(bytes.NewReader(testMBText)); err != nil {
		t.Fatal(err)
	}
}

func TestGetMethodHandler(t *testing.T) {
	var test = func(method string, header http.Header, expected reflect.Type) {
		all := &allInterfaces{}
		h := getMethodHandler(all, method, header)
		if h == nil {
			t.Errorf("handler for %s returned nil", method)
		}

		if ht := reflect.TypeOf(h); ht != expected {
			t.Errorf("handler for %s returned %s when %s was expected", method, ht, expected)
		}
	}
	test(Options, nil, tOptionsHandler)
	test(Head, nil, tGetFunc)
	test(Get, nil, tGetFunc)
	test(Patch, nil, tPatchFunc)
	test(Put, nil, tPutFunc)
	test(Post, nil, tPostFunc)
	test(Delete, nil, tDeleteFunc)
}

func TestMethodNotAllowed(t *testing.T) {
	rr := newRequestResponse(Delete, testServerAddr+"/people", nil, nil)
	if err := rr.TestStatusCode(http.StatusMethodNotAllowed); err != nil {
		t.Fatal(err)
	}
}

func TestOptionsHandler(t *testing.T) {
	rr := newRequestResponse(Options, testServerAddr+"/people", nil, nil)
	if err := rr.TestStatusCode(http.StatusNoContent); err != nil {
		t.Fatal(err)
	}
	allowed := strings.Join([]string{Head, Get, Post}, ", ")
	if err := rr.TestHeader("Allow", allowed); err != nil {
		t.Fatal(err)
	}
}

func TestGetHandler(t *testing.T) {
	var test = func(method string) *requestResponse {
		header := make(http.Header)
		header.Set("Accept", testContentType)
		rr := newRequestResponse(method, testServerAddr+"/people", header, nil)
		if err := rr.TestStatusCode(http.StatusOK); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeader("Content-Type", header.Get("Accept")); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHasHeader("Last-Modified"); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHasHeader("Etag"); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHasHeader("Expires"); err != nil {
			t.Fatal(err)
		}
		return rr

	}

	headRR := test(Head)
	if err := headRR.TestBody(bytes.NewBufferString("")); err != nil {
		t.Fatal(err)
	}

	getRR := test(Get)
	if err := getRR.TestBody(bytes.NewBufferString(testCannedContent)); err != nil {
		t.Fatal(err)
	}
}

func TestExpires(t *testing.T) {
	header := make(http.Header)
	rr := newRequestResponse(Get, testServerAddr+"/people/"+testPeople[0].ID, header, nil)
	if err := rr.TestStatusCode(http.StatusOK); err != nil {
		t.Fatal(err)
	}

	d, err := time.Parse(rfc1123, rr.resp.Header.Get("Date"))
	if err != nil {
		t.Fatal(err)
	}
	d = d.Add(testPeople[0].TTL())

	if err := rr.TestHeader("Expires", d.UTC().Format(rfc1123)); err != nil {
		t.Fatal(err)
	}
}

func TestGetConditional(t *testing.T) {
	var test = func(method string, date time.Time, expected int) *requestResponse {
		header := make(http.Header)
		header.Set("If-Modified-Since", date.UTC().Format(rfc1123))
		rr := newRequestResponse(method, testServerAddr+"/people/"+testPeople[0].ID, header, nil)
		if err := rr.TestStatusCode(expected); err != nil {
			t.Fatal(err)
		}
		return rr
	}

	test(Head, time.Now(), http.StatusNotModified)
	test(Get, time.Now(), http.StatusNotModified)
	test(Head, testTimeReference, http.StatusNotModified)
	test(Get, testTimeReference, http.StatusNotModified)
	test(Head, testTimeReference.Add(-24*time.Hour), http.StatusOK)
	test(Get, testTimeReference.Add(-24*time.Hour), http.StatusOK)
}

// Get with invalid Range header should behave like a normal Get.
func TestGetInvalidRangeHandler(t *testing.T) {
	var test = func(method string) {
		header := make(http.Header)
		header.Set("Accept", "application/json")
		header.Set("Range", "blablabla")
		rr := newRequestResponse(Get, testServerAddr+"/people", header, nil)

		if err := rr.TestStatusCode(http.StatusOK); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeader("Content-Type", "application/json; charset=utf-8"); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHasNoHeader("Content-Range"); err != nil {
			t.Fatal(err)
		}
	}
	test(Head)
	test(Get)
}

func TestPartialGetHandler(t *testing.T) {
	var test = func(method string) {
		header := make(http.Header)
		header.Set("Accept", "application/json")
		header.Set("Range", "resources=0-39")
		rr := newRequestResponse(Get, testServerAddr+"/people", header, nil)

		if err := rr.TestStatusCode(http.StatusPartialContent); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeader("Content-Type", "application/json; charset=utf-8"); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeader("Content-Range", fmt.Sprintf("resources 0-39/%d", len(testPeopleResourceCollection))); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeaderContains("Vary", "Range"); err != nil {
			t.Fatal(err)
		}
	}
	test(Head)
	test(Get)
}

func TestIfRangeGetHander(t *testing.T) {
	var test = func(ifRange string, expected int) {
		header := make(http.Header)
		header.Set("Accept", "application/json")
		header.Set("Range", "resources=0-39")
		header.Set("If-Range", ifRange)
		rr := newRequestResponse(Get, testServerAddr+"/people", header, nil)

		if err := rr.TestStatusCode(expected); err != nil {
			t.Fatal(err)
		}
	}
	// Matching ETag value
	test(testPeopleResourceCollection.ETag(), http.StatusPartialContent)
	// Matching Last Modification date
	test(testPeopleResourceCollection.LastModified().UTC().Format(rfc1123), http.StatusPartialContent)
	// Non-Matching value
	test("blablabla", http.StatusOK)
}

func TestPartialGetNotSatisfiableHandler(t *testing.T) {
	var test = func(method string) {
		header := make(http.Header)
		header.Set("Accept", "application/json")
		header.Set("Range", "resources=10000-20000")
		rr := newRequestResponse(Get, testServerAddr+"/people", header, nil)

		if err := rr.TestStatusCode(http.StatusRequestedRangeNotSatisfiable); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHeader("Content-Range", fmt.Sprintf("*/%d", len(testPeopleResourceCollection))); err != nil {
			t.Fatal(err)
		}
	}
	test(Head)
	test(Get)
}

func TestPartialGetUnsupportedUnit(t *testing.T) {
	var test = func(method string) {
		header := make(http.Header)
		header.Set("Accept", "application/json")
		header.Set("Range", "unsupported=0-20")
		rr := newRequestResponse(Get, testServerAddr+"/people", header, nil)

		if err := rr.TestStatusCode(http.StatusOK); err != nil {
			t.Fatal(err)
		}
		if err := rr.TestHasNoHeader("Content-Range"); err != nil {
			t.Fatal(err)
		}
	}
	test(Head)
	test(Get)
}

func TestDelete(t *testing.T) {
	rr := newRequestResponse(Delete, testServerAddr+"/people/"+testPeople[0].ID, nil, nil)
	if err := rr.TestStatusCode(http.StatusNoContent); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestBody(bytes.NewBufferString("")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteNotFound(t *testing.T) {
	rr := newRequestResponse(Delete, testServerAddr+"/people/blablabla", nil, nil)
	if err := rr.TestStatusCode(http.StatusNotFound); err != nil {
		t.Fatal(err)
	}
}

func TestPostUnsupportedMediaType(t *testing.T) {
	header := make(http.Header)
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "blabla")
	rr := newRequestResponse(Post, testServerAddr+"/people", header, nil)
	if err := rr.TestStatusCode(http.StatusUnsupportedMediaType); err != nil {
		t.Fatal(err)
	}
}

func TestPost(t *testing.T) {
	header := make(http.Header)
	header.Set("Accept", "application/json")
	header.Set("Content-Type", "application/json")
	rr := newRequestResponse(Post, testServerAddr+"/people", header, nil)
	if err := rr.TestStatusCode(http.StatusCreated); err != nil {
		t.Fatal(err)
	}
	if err := rr.TestHeader("Location", "https://"); err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(testPeople[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := rr.TestBody(bytes.NewBuffer(b)); err != nil {
		t.Fatal(err)
	}
}
