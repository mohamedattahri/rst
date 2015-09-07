package rst

import (
	"net/http"
	"testing"
)

var testCORSHeaders = []string{
	"Origin",
	"Access-Control-Allow-Credentials",
	"Access-Control-Expose-Headers",
	"Access-Control-Allow-Methods",
	"Access-Control-Allow-Headers",
	"Access-Control-Max-Age",
}

func TestSimpleRequestDisabled(t *testing.T) {
	testMux.SetCORSPolicy(nil)

	test := func(url string) {
		header := make(http.Header)
		header.Set("Origin", "example.com")
		rr := newRequestResponse(Get, url, header, nil)

		if err := rr.TestStatusCode(200); err != nil {
			t.Fatal("CORS Get Response:", err)
		}

		for _, item := range testCORSHeaders {
			if err := rr.TestHasNoHeader(item); err != nil {
				t.Fatal("CORS simple request:", err)
			}
		}
	}

	test(testSafeURL)
	test(testBypassURL)
}

func TestSimpleRequestPermissive(t *testing.T) {
	testMux.SetCORSPolicy(PermissiveAccessControl)

	test := func(url string) {
		header := make(http.Header)
		header.Set("Origin", "example.com")
		rr := newRequestResponse(Get, url, header, nil)

		if err := rr.TestStatusCode(200); err != nil {
			t.Fatal("CORS Get Response:", err)
		}

		if err := rr.TestHeader("Access-Control-Allow-Origin", "*"); err != nil {
			t.Fatal("CORS simple request:", err)
		}
	}

	test(testSafeURL)
	test(testBypassURL)
}

func TestPreflightRequestPermissive(t *testing.T) {
	testMux.SetCORSPolicy(PermissiveAccessControl)

	header := make(http.Header)
	header.Set("Origin", "example.com")
	header.Set("Access-Control-Allow-Crentials", Head)
	header.Set("Access-Control-Request-Method", Head)
	header.Set("Access-Control-Request-Headers", "X-Custom-Header-1, X-Custom-Header-2")
	rr := newRequestResponse(Options, testSafeURL, header, nil)

	if err := rr.TestStatusCode(204); err != nil {
		t.Fatal("CORS Options Response:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Origin", "*"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Methods", rr.resp.Header.Get("Allow")); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Headers", header.Get("Access-Control-Request-Headers")); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Credentials", "true"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}
}

func TestSimpleRequestDefault(t *testing.T) {
	testMux.SetCORSPolicy(DefaultAccessControl)

	header := make(http.Header)
	header.Set("Origin", "example.com")
	rr := newRequestResponse(Get, testSafeURL, header, nil)

	if err := rr.TestStatusCode(200); err != nil {
		t.Fatal("CORS Get Response:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Origin", "*"); err != nil {
		t.Fatal("CORS simple request:", err)
	}
}

func TestPreflightRequestDefault(t *testing.T) {
	testMux.SetCORSPolicy(DefaultAccessControl)

	header := make(http.Header)
	header.Set("Origin", "example.com")
	header.Set("Access-Control-Allow-Crentials", Head)
	header.Set("Access-Control-Request-Method", Head)
	header.Set("Access-Control-Request-Headers", "X-Custom-Header-1, X-Custom-Header-2")
	rr := newRequestResponse(Options, testSafeURL, header, nil)

	if err := rr.TestStatusCode(204); err != nil {
		t.Fatal("CORS Options Response:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Origin", "*"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHasNoHeader("Access-Control-Allow-Methods"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHasNoHeader("Access-Control-Allow-Headers"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Credentials", "true"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}
}

func TestSimpleRequestCustom(t *testing.T) {
	origin := "something.com"
	testMux.SetCORSPolicy(&AccessControlResponse{
		Origin: origin,
	})

	header := make(http.Header)
	header.Set("Origin", "example.com")
	rr := newRequestResponse(Get, testSafeURL, header, nil)

	if err := rr.TestStatusCode(200); err != nil {
		t.Fatal("CORS Get Response:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Origin", origin); err != nil {
		t.Fatal("CORS simple request:", err)
	}
}

func TestPreflightedRequestCustom(t *testing.T) {
	origin := "something.com"
	testMux.SetCORSPolicy(&AccessControlResponse{
		Origin:         origin,
		AllowedHeaders: []string{"X-Custom-Header-1", "X-Custom-Header-2"},
		ExposedHeaders: []string{"X-Custom-Header-1"},
	})

	header := make(http.Header)
	header.Set("Origin", "example.com")
	header.Set("Access-Control-Allow-Crentials", Head)
	header.Set("Access-Control-Request-Method", Head)
	header.Set("Access-Control-Request-Headers", "X-Custom-Header-1, X-Custom-Header-2")
	rr := newRequestResponse(Options, testSafeURL, header, nil)

	if err := rr.TestStatusCode(204); err != nil {
		t.Fatal("CORS Get Response:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Origin", origin); err != nil {
		t.Fatal("CORS simple request:", err)
	}

	if err := rr.TestHasNoHeader("Access-Control-Allow-Methods"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Expose-Headers", "X-Custom-Header-1"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}

	if err := rr.TestHeader("Access-Control-Allow-Credentials", "false"); err != nil {
		t.Fatal("CORS preflighted request:", err)
	}
}

func TestPreflightInterface(t *testing.T) {
	testMux.SetCORSPolicy(&AccessControlResponse{
		Origin: "custom.example.com",
	})
	header := make(http.Header)
	header.Set("Origin", "example.com")
	rr := newRequestResponse(Options, testServerAddr+"/echo", header, nil)
	if err := rr.TestHeader("Access-Control-Allow-Origin", "preflighted.domain.com"); err != nil {
		t.Fatal(err)
	}
}
