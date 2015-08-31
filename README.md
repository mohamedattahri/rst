# github.com/mohamedattahri/rst

[![GoDoc](https://godoc.org/github.com/mohamedattahri/rst?status.svg)](https://godoc.org/github.com/mohamedattahri/rst)
[![Build Status](https://travis-ci.org/mohamedattahri/rst.svg?branch=master)](https://travis-ci.org/mohamedattahri/rst)  

`rst` implements tools and methods to expose resources in a RESTFul service.

## Test Coverage

`go test -cover` reports **78.3%**.

## Getting started

The idea behind `rst` is to have endpoints and resources implement interfaces to add support for HTTP features.

Endpoints can implement [Getter](#getter), [Poster](#poster), [Patcher](#patcher), [Putter](#putter) or [Deleter](#deleter) to respectively allow the `HEAD`/`GET`, `POST`, `PATCH`, `PUT`, and `DELETE` HTTP methods.

Resources can implement [Ranger](#ranger) to support partial `GET` requests, [Marshaler](#marshaler) to customize the process with which they are encoded, or [http.Handler](#http.handler) to have a complete control over the ResponseWriter.

With these interfaces, the complexity behind dealing with all the headers and status codes of the HTTP protocol is abstracted to let you focus on returning a resource or an error.

### Resources

A resource must implement the `rst.Resource` interface.

For that, you can either wrap an `rst.Envelope` around an existing type,
or define a new type and implement the methods of the interface yourself.

Using a `rst.Envelope`:

```go
projection := map[string]string{
	"ID"	: "a1-b2-c3-d4-e5-f6",
	"Name"	: "Francis Underwood",
}
lastModified := time.Now()
etag := fmt.Sprintf("%d-%s", lastModified.Unix(), projection["ID"])
ttl = 10 * time.Minute

resource := rst.NewEnvelope(
	projection,
	lastModified,
	etag,
	ttl,
)
```

Using a struct:

```go
type Person struct {
    ID string
    Name string
    modifiedDate time.Time
}

// This will be helpful for conditional GETs
// and to detect conflicts before PATCHs for example.
func (p *Person) LastModified() time.Time {
    return p.modifiedDate
}

// An ETag inspired by Facebook.
func (p *Person) ETag() string {
    return fmt.Sprintf("%d-%s", p.LastModified().Unix(), p.ID)
}

// This value will help set the Expires header and
// improve the cacheability of this resource.
func (p *Person) TTL() time.Duration {
    return 10 * time.Second
}

resource := &Person{
	ID: "a1-b2-c3-d4-e5-f6",
	Name: "Francis Underwood",
	modifiedDate: time.Now(),
}
```

### Endpoints

An endpoint is an access point to a resource in your service.

You can either define an endpoint by defining handlers for different methods
sharing the same pattern, or by submitting a type that implements `Getter`, `Poster`,
`Patcher`, `Putter`, `Deleter` and/or `Prefligher`.

Using rst.Mux:
```go
mux := rst.NewMux()
mux.Get("/people/{id:\\d+}", func(vars RouteVars, r *http.Request) (rst.Resource, error) {
	resource := database.Find(vars.Get("id"))
	if resource == nil {
		return nul, rst.NotFound()
	}
	return resource, nil
})
mux.Delete("/people/{id:\\d+}", func(vars RouteVars, r *http.Request) error {
	resource := database.Find(vars.Get("id"))
	if resource == nil {
		return nul, rst.NotFound()
	}
	return resource.Delete()
})
```

Using a struct:

```go
type PersonEP struct {}

func (ep *PersonEP) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
	resource := database.Find(vars.Get("id"))
	if resource == nil {
		return nil, rst.NotFound()
	}
	return resource, nil
}

func (ep *PersonEP) Delete(vars rst.RouteVars, r *http.Request) error {
	resource := database.Find(vars.Get("id"))
	if resource == nil {
		return nil, rst.NotFound()
	}
	return resource.Delete()
}
```

### Routing

Routing of requests in `rst` is powered by [Gorilla mux](https://github.com/gorilla/mux). Only URL patterns are available for now. Optional regular expressions are supported.

```go
mux := rst.NewMux()
mux.Debug = true // make sure this is switched back to false before production

// Headers set in mux are added to all responses
mux.Header().Set("Server", "Awesome Service Software 1.0")
mux.Header().Set("X-Powered-By", "rst")

mux.Handle("/people/{id:\\d+}", rst.EndpointHandler(&PersonEP{}))

http.ListenAndServe(":8080", mux)
```

### Encoding

`rst` supports JSON, XML and text encoding of resources using the encoders in Go's standard library.

It negotiates the right encoding format based on the content of the `Accept` header in the request, calls the appropriate marshaler, and inserts the result in a response with the right status code and headers.

Media MIME type    |	Encoder
-------------------|-------------
application/json   |	json
text/javascript    |	json
application/xml    |	xml
text/xml           |	xml
text/plain         |	text
\*/\*              |	json

You can implement the `Marshaler` interface if you want to add support for another format, or for more control over the encoding process of a specific resource.

### Compression

`rst` compresses the payload of responses using the supported algorithm detected in the request's `Accept-Encoding` header.

Payloads under `CompressionThreshold` bytes are not compressed.

Both Gzip and Flate are supported.

## Features

### Options

`OPTIONS` requests are implicitly supported by all endpoints.

### Cache

The `ETag`, `Last-Modified` and `Vary` headers are automatically set.

`rst` responds with `304 NOT MODIFIED` when an appropriate `If-Modified-Since` or `If-None-Match` header is found in the request.

The `Expires` header is also automatically inserted with the duration returned by `Resource.TTL()`.

### Partial Gets

A resource can implement the [Ranger](#ranger) interface to gain the ability to return partial responses with status code `206 PARTIAL CONTENT` and `Content-Range` header automatically inserted.

`Ranger.Range` method will be called when a valid `Range` header is found in an incoming `GET` request.

The `Accept-Ranges` header will be inserted automatically.

The supported range units and the range extent will be validated for you.

Note that the `If-Range` conditional header is supported as well.

### CORS

`rst` can add the headers required to serve cross-origin (CORS) requests for you.

You can choose between two provided policies (`DefaultAccessControl` and `PermissiveAccessControl`), or define your own.

```go
mux.SetCORSPolicy(rst.PermissiveAccessControl)
```

Support can be disabled by passing `nil`.

Preflighted requests are also supported. However, you can customize the responses returned by preflight `OPTIONS` requests if you implement the `Preflighter` interface in your endpoint.

## Interfaces

### Endpoints

#### <a id="getter"></a>Getter

Getter allows `GET` and `HEAD` method requests.

```go
func (ep *endpoint) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
    resource := database.Find(vars.Get("id"))
    if resource == nil {
        return nil, rst.NotFound()
    }
    return resource, nil
}
```

#### <a id="poster"></a>Poster

Poster allows an endpoint to handle `POST` requests.

```go
func (ep *endpoint) Post(vars rst.RouteVars, r *http.Request) (rst.Resource, string, error) {
	resource, err := newResourceFromRequest(r)
	if err != nil {
		return nil, "", err
	}
	uri := "https://example.com/resource/" + resource.ID
    return resource, uri, nil
}
```

#### <a id="patcher"></a>Patcher

Patcher allows an endpoint to handle `PATCH` requests.

```go
func (ep *endpoint) Patch(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
    resource := database.Find(vars.Get("id"))
    if resource == nil {
        return nil, rst.NotFound()
    }

    if r.Header.Get("Content-Type") != "application/www-form-urlencoded" {
    	return nil, rst.UnsupportedMediaType("application/www-form-urlencoded")
    }

    // Detect any writing conflicts
    if rst.ValidateConditions(resource, r) {
		return nil, rst.PreconditionFailed()
    }

    // Read r.Body and apply changes to resource
    // then return it
    return resource, nil
}
```

#### <a id="putter"></a>Putter

Putter allows an endpoint to handle `PUT` requests.

```go
func (ep *endpoint) Put(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
    resource := database.Find(vars.Get("id"))
    if resource == nil {
        return nil, rst.NotFound()
    }

    // Detect any writing conflicts
    if rst.ValidateConditions(resource, r) {
		return nil, rst.PreconditionFailed()
    }

    // Read r.Body and apply changes to resource
    // then return it
    return resource, nil
}
```

#### <a id="deleter"></a>Deleter

Deleter allows an endpoint to handle `DELETE` requests.

```go
func (ep *endpoint) Delete(vars rst.RouteVars, r *http.Request) error {
    resource := database.Find(vars.Get("id"))
    if resource == nil {
        return rst.NotFound()
    }
    return nil
}
```

#### <a id="preflighter"></a>Preflighter

Preflighter allows you to customize the CORS headers returned to an `OPTIONS` preflight request sent by user agents before the actual request.

For the endpoint in this example, different policies are implemented for different times of the day.

```go
func (e *endpoint) Preflight(req *rst.AccessControlRequest, vars rst.RouteVars, r *http.Request) *rst.AccessControlResponse {
	if time.Now().Hour() < 12 {
		return &rst.AccessControlResponse{
			Origin: "morning.example.com",
			Methods: []string{"GET"},
		}
	}

	return &rst.AccessControlResponse{
		Origin: "afternoon.example.com",
		Methods: []string{"POST"},
	}
}
```

### Resources

#### <a id="ranger"></a>Ranger

Resources that implement Ranger can handle requests with a `Range` header and return partial responses with status code `206 PARTIAL CONTENT`. It's the HTTP solution to pagination.

```go
type Doc []byte
// assuming Doc implements rst.Resource interface

// Supported units will be displayed in the Accept-Range header
func (d *Doc) Units() []string {
    return []string{"bytes"}
}

// Count returns the total number of range units available
func (d *Doc) Count() uint64 {
	return uint64(len(d))
}

func (d *Doc) Range(rg *rst.Range) (*rst.ContentRange, rst.Resource, error) {
	cr := &ContentRange{rg, c.Count()}
	part := d[rg.From : rg.To+1]
	return cr, part, nil
}
```

#### <a id="marshaler"></a>Marshaler

Marshaler allows you to control the encoding of a resource and return the array of bytes that will form the payload of the response.

`MarshalRST` is to `rst.Marshal` what `MarshalJSON` is to `json.Marshal`.

```go
const png = "image/png"

type User struct{}
// assuming User implements rst.Resource

// MarshalRST returns the profile picture of the user if the Accept header
// of the request indicates "image/png", and relies on rst.MarshalResource
// to handle the other cases.
func (u *User) MarshalRST(r *http.Request) (string, []byte, error) {
	accept := rst.ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate(png) == png {
		b, err := ioutil.ReadFile("path/of/user/profile/picture.png")
		return png, b, err
	}

	return rst.MarshalResource(u, r)
}
```

#### <a id="http.handler"></a>http.Handler

http.Handler is a low level solution for when you need
complete control over the process by which a resource
is written in the response's payload.

In the following example, http.Handler is implemented to return a chunked response.

```go
type User struct{}
// assuming User implements rst.Resource

// ServeHTTP will send half the data now, and the
// rest 10 seconds later.
func (u *User) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    ct, b, err := rst.MarshalResource(u, r)
    if err != nil {
        rst.ErrorHandler(err).ServeHTTP(w, r)
        return
    }
    w.Header.Set("Content-Type", ct)

    half := len(b) / 2
    w.Write(b[:half])
    time.Sleep(10 *time.Second)
    w.Write(b[half:])
}
```

## Debugging and Recovering from errors

Set `mux.Debug` to `true` and `rst` will recover from panics and errors with status code 500 to display a useful page with the full stack trace and info about the request.

![alt tag](/internal/assets/recover.jpg)
