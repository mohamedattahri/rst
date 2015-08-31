package rst

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

var (
	testHost                     = "127.0.0.1:55839"
	testServerAddr               = "http://" + testHost
	testMux                      *Mux
	testMBText                   []byte
	testSafeURL                  string
	testEchoURL                  string
	testEnvelopeURL              string
	testBypassURL                string
	testPeople                   []*person
	testPeopleResourceCollection resourceCollection
)

const (
	testContentType   = "hello/world"
	testCannedContent = "hello, world!"
)

var (
	testCannedBytes   = []byte(testCannedContent)
	testTimeReference = time.Date(2014, 4, 14, 10, 0, 0, 0, time.UTC)
)

type employer struct {
	Company   string `json:"company"`
	Continent string `json:"continent"`
}

func (e *employer) LastModified() time.Time {
	return testTimeReference
}

func (e *employer) ETag() string {
	return fmt.Sprintf("%s-%d", e.Company, e.LastModified().Unix())
}

func (e *employer) TTL() time.Duration {
	return 15 * time.Minute
}

func (e *employer) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate(testContentType) == testContentType {
		return testContentType, testCannedBytes, nil
	}
	return MarshalResource(e, r)
}

type person struct {
	ID        string    `json:"_id"`
	Age       int       `json:"age"`
	EyeColor  string    `json:"eyeColor"`
	Firstname string    `json:"firstname"`
	Lastname  string    `json:"lastname"`
	Employer  *employer `json:"employer"`
}

func (p *person) LastModified() time.Time {
	return testTimeReference
}

func (p *person) ETag() string {
	return fmt.Sprintf("%s-%d", p.ID, p.LastModified().Unix())
}

func (p *person) TTL() time.Duration {
	return 30 * time.Second
}

func (p *person) String() string {
	return fmt.Sprintf("%s %s (%s)", p.Firstname, p.Lastname, p.EyeColor)
}

func (p *person) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate(testContentType) == testContentType {
		return testContentType, testCannedBytes, nil
	}
	return MarshalResource(p, r)
}

type resourceCollection []Resource

func (c resourceCollection) Count() uint64 {
	return uint64(len(c))
}

func (c resourceCollection) Units() []string {
	return []string{"bytes", "resources"}
}

func (c resourceCollection) Range(rg *Range) (*ContentRange, Resource, error) {
	return &ContentRange{rg, c.Count()}, c[rg.From : rg.To+1], nil
}

func (c resourceCollection) LastModified() time.Time {
	return testTimeReference
}

func (c resourceCollection) ETag() string {
	if len(c) == 0 {
		return "*"
	}
	return c[0].ETag()
}

func (c resourceCollection) TTL() time.Duration {
	return 15 * time.Second
}

func (c resourceCollection) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate(testContentType) == testContentType {
		return testContentType, testCannedBytes, nil
	}
	return MarshalResource(c, r)
}

type echoResource struct {
	content []byte
}

func (e *echoResource) MarshalRST(r *http.Request) (string, []byte, error) {
	return "text/plain", e.content, nil
}

func (e *echoResource) LastModified() time.Time {
	return testTimeReference
}

func (e *echoResource) ETag() string {
	return "*"
}

func (e *echoResource) TTL() time.Duration {
	return 0
}

type chunckedEchoResource struct {
	content []byte
}

func (e *chunckedEchoResource) LastModified() time.Time {
	return testTimeReference
}

func (e *chunckedEchoResource) ETag() string {
	return "*"
}

func (e *chunckedEchoResource) TTL() time.Duration {
	return 0
}

// ServeHTTP will write 1/10th of e.content every 5 seconds.
func (e *chunckedEchoResource) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Write(e.content)
}

type echoEndpoint struct{}

// Post will simply return any data found in the body of the request.
func (ec *echoEndpoint) Post(vars RouteVars, r *http.Request) (Resource, string, error) {
	c, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	defer r.Body.Close()
	return &echoResource{c}, "", nil
}

func (ec *echoEndpoint) Preflight(acReq *AccessControlRequest, vars RouteVars, r *http.Request) *AccessControlResponse {
	return &AccessControlResponse{
		Origin: "preflighted.domain.com",
	}
}

type chunkedEchoEndpoint struct{}

// Post will simply return any data found in the body of the request.
func (ec *chunkedEchoEndpoint) Post(vars RouteVars, r *http.Request) (Resource, string, error) {
	c, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, "", err
	}
	defer r.Body.Close()
	return &chunckedEchoResource{c}, "", nil
}

type panicEndpoint struct{}

// Post will simply return any data found in the body of the request.
func (ep *panicEndpoint) Get(vars RouteVars, r *http.Request) (Resource, error) {
	panic(errors.New("provoked panic"))
}

type peopleCollection struct{}

// Get returns the content of testPeople.
func (c *peopleCollection) Get(vars RouteVars, r *http.Request) (Resource, error) {
	col := make(resourceCollection, len(testPeople))
	for i, p := range testPeople {
		col[i] = Resource(p)
	}
	if col.Count() == 0 {
		return nil, nil
	}
	return col, nil
}

// Post returns the first item in testPeople as if it was just created.
func (c *peopleCollection) Post(vars RouteVars, r *http.Request) (Resource, string, error) {
	if r.Header.Get("Content-Type") != "application/json" {
		return nil, "", UnsupportedMediaType("application/json")
	}

	return testPeople[0], "https://", nil
}

type personResource struct{}

func (e *personResource) Get(vars RouteVars, r *http.Request) (Resource, error) {
	for _, p := range testPeople {
		if p.ID == vars.Get("id") {
			return p, nil
		}
	}
	return nil, NotFound()
}

func (e *personResource) Delete(vars RouteVars, r *http.Request) error {
	for i, p := range testPeople {
		if p.ID == vars.Get("id") {
			testPeople = append(testPeople[:i], testPeople[i+1:]...)
			return nil
		}
	}
	return NotFound()
}

type employersCollection struct{}

func (c *employersCollection) Get(vars RouteVars, r *http.Request) (Resource, error) {
	index := make(map[string]Resource)
	for _, p := range testPeople {
		index[p.Employer.Company] = Resource(p.Employer)
	}
	if len(index) == 0 {
		return nil, nil
	}

	col := make(resourceCollection, len(index))
	for _, resource := range index {
		col = append(col, resource)
	}
	return col, nil
}

type employerResource struct{}

func (e *employerResource) Get(vars RouteVars, r *http.Request) (Resource, error) {
	for _, p := range testPeople {
		if strings.EqualFold(p.Employer.Company, vars.Get("name")) {
			return p.Employer, nil
		}
	}
	return nil, NotFound()
}

type testProjection map[string]string

func (t testProjection) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate("text/plain") == "text/plain" {
		return "text/plain", []byte(envelopeTextProjection), nil
	}
	return MarshalResource(t, r)
}

var (
	envelopeProjection = testProjection{
		"manufacturer": "Gibson",
		"model":        "LesPaul 1968",
	}
	envelopeTextProjection = "hello, world"
	envelopeTTL            = 10 * time.Minute
	envelopeETag           = "envelope-etag"
	envelopeLastModified   = time.Date(1989, time.April, 14, 9, 0, 0, 0, time.UTC)
	envelopeHeaders        = http.Header{
		"X-Envelope-Header": []string{"some-value"},
	}
)

type envelopeEndpoint struct{}

func (e *envelopeEndpoint) Get(vars RouteVars, r *http.Request) (Resource, error) {
	evlp := NewEnvelope(
		envelopeProjection,
		envelopeLastModified,
		envelopeETag,
		envelopeTTL,
	)
	evlp.header = envelopeHeaders
	return evlp, nil
}

func TestMain(m *testing.M) {
	var err error

	// 1MB text
	testMBText, err = ioutil.ReadFile("internal/testdata/1mb.txt")
	if err != nil {
		log.Fatal(err)
	}

	// DB
	rawdb, err := ioutil.ReadFile("internal/testdata/100objects.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(rawdb, &testPeople); err != nil {
		log.Fatal(err)
	}
	for _, p := range testPeople {
		testPeopleResourceCollection = append(testPeopleResourceCollection, Resource(p))
	}

	testMux = NewMux()
	testMux.Debug = true

	testMux.Handle("/bypass", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(testCannedBytes)
	}))

	testMux.Handle("/echo", EndpointHandler(&echoEndpoint{}))
	testMux.Handle("/envelope", EndpointHandler(&envelopeEndpoint{}))
	testMux.Handle("/chunked", EndpointHandler(&chunkedEchoEndpoint{}))
	testMux.Handle("/panic", EndpointHandler(&panicEndpoint{}))
	testMux.Handle("/people", EndpointHandler(&peopleCollection{}))
	testMux.Handle("/people/{id}", EndpointHandler(&personResource{}))
	testMux.Handle("/employers", EndpointHandler(&employersCollection{}))
	testMux.Handle("/employers/{name}", EndpointHandler(&employerResource{}))
	go http.ListenAndServe(testHost, testMux)

	testBypassURL = testServerAddr + "/bypass"
	testEchoURL = testServerAddr + "/echo"
	testEnvelopeURL = testServerAddr + "/envelope"
	testSafeURL = testServerAddr + "/people"

	os.Exit(m.Run())
}
