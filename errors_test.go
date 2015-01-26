package rst

import (
	"bytes"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"testing"
)

// TestInternalServerErrorStack tests whether the stack is only visible when
// debug is true.
func TestInternalServerErrorStackDisplay(t *testing.T) {
	var test = func(expected bool) {
		header := make(http.Header)
		header.Set("Accept", "*/*")
		rr := newRequestResponse(Get, testServerAddr+"/panic", header, nil)
		if err := rr.TestStatusCode(http.StatusInternalServerError); err != nil {
			t.Fatal(err)
		}

		if err := rr.TestHeaderContains("Content-Type", "text/html"); err != nil {
			t.Fatal(err)
		}

		b, err := ioutil.ReadAll(rr.resp.Body)
		rr.resp.Body.Close()
		if err != nil {
			t.Fatal("expected response to have body")
		}

		if got := strings.Contains(string(b), "<h2>Stack</h2>"); got != expected {
			t.Fatal("Contains stack. Got:", got, "Wanted:", expected)
		}
	}

	logger := testMux.Logger
	debug := testMux.Debug
	defer func() {
		testMux.Logger = logger
		testMux.Debug = debug
	}()

	buffer := new(bytes.Buffer)

	buffer.Reset()
	testMux.Logger = log.New(ioutil.Discard, "", log.Ltime)
	testMux.Debug = true
	test(testMux.Debug)

	buffer.Reset()
	testMux.Logger = log.New(buffer, "", log.Ltime)
	testMux.Debug = false
	test(testMux.Debug)
	if !strings.Contains(buffer.String(), "500 (Internal Server Error)") {
		t.Fatalf("provoked panic with Debug=False did not log message correctly: %s", buffer.String())
	}
}
