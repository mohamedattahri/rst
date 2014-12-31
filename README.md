# github.com/mohamedattahri/rst

[![Build Status](https://travis-ci.org/mohamedattahri/rst.svg?branch=master)](https://travis-ci.org/mohamedattahri/rst)  [![GoDoc](https://godoc.org/github.com/mohamedattahri/rst?status.svg)](https://godoc.org/github.com/mohamedattahri/rst)

`rst` implements tools and methods to expose resources in a RESTFul service.

## Getting started

The idea behind `rst` is to have endpoints and resources implementing interfaces to add features.

Endpoints can implement [Getter](#getter), [Poster](#poster), [Patcher](#patcher), [Putter](#putter) or [Deleter](#deleter) to respectively allow the `GET`, `PATCH`, `POST`, `PUT`, and `DELETE` HTTP methods.

Resources can implement [Ranger](#ranger) to support partial `GET` requests, or [Marshaler](#marshaler) to customize the process with which they are encoded.

With these interfaces, the complexity behind dealing with all the headers and status codes of the HTTP protocol is abstracted to let you focus on returning a resource or an error.

### Resources

A resource must implement the `Resource` interface.

Here's a basic example:

```go
type Person struct {
    ID string
    Name string
    ModifiedDate time.Time `json:"-" xml:"-"`
}

// This will be helpful for conditional GETs
// and to detect conflicts before PATCHs for example.
func (p *Person) LastModified() time.Time {
    return p.ModifiedDate
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

```

### Endpoints

An endpoint is an access point to a resource in your service.

In the following example, `PersonEP` implements `Getter` and is therefore able to handle `GET` requests.

```go
type PersonEP struct {}

func (ep *PersonEP) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
	resource := database.Find(vars.Get("id"))
	if resource == nil {
		return nil, rst.NotFound()
	}
	return resource, nil
}
```

`Get` uses the `id` variable extracted from the URL to load a resource from the database, or return a `404 Not Found` error.

### Routing

Routing of requests in `rst` is powered by [Gorilla mux](https://github.com/gorilla/mux). Only URL patterns are available for now. Optional regular expressions are supported.

```go
mux := rst.NewRESTMux()

// Headers set in mux are added to all responses
mux.Header().Set("Server", "Awesome Service Software 1.0")
mux.Header().Set("X-Powered-By", "rst")

mux.Handle("/people/{id:\\d+}", rst.EndpointHandler(&PersonEP{}))

http.ListenAndServe(":8080", mux)
```

At this point, our service only allows `GET` requests on a resource called `Person`.

### Encoding

`rst` supports JSON, XML and text encoding of resources using the encoders in Go's standard library.


It negotiates the right encoding format based on the content of the `Accept` header in the request, calls the appropriate marshaler, and inserts the result in a response with the right status code and headers.

Media MIME type    |	Encoder
-                  |    -
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

`OPTIONS` requests are implicitely supported by all endpoints

### Cache

The `ETag`, `Last-Modified` and `Vary` headers are automatically set.

`rst` responds with `304 NOT MODIFIED` when an appropriate `If-Modified-Since` or `If-None-Match` header is found in the request.

The `Expires` header is also automatically inserted with the duration returned by `resource.TTL()`.

### Partial Gets

A resource can implement the [Ranger](#ranger) interface to gain the ability to return partial responses with status code `206 PARTIAL CONTENT` and `Content-Range` header automatically inserted.

Now that `Ranger` is implemented, the `Range` method will be called when a valid `Range` header is found in an incoming `GET` request.

Note that the `If-Range` conditional header is supported as well.


### CORS

`rst` can add the headers required to serve cross-origin (CORS) requests for you.

You can choose between two provided policies (`DefaultAccessControl` and `PermissiveAccessControl`), or define your own.

```go
mux.SetCORSPolicy(rst.PermissiveAccessControl)
```

Support can be disabled by passing `nil`.

Preflighted requests are also supported. However, you can customize the responses returned by the preflight `OPTIONS` requests by implementing the `Preflighter` interface in your endpoint.

## Interfaces

### Endpoints

#### <a id="getter"></a>Getter

Getter allows `GET` and `HEAD` requests for a method.

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
func (ep *endpoint) Get(vars rst.RouteVars, r *http.Request) (rst.Resource, string, error) {
	resource, err := NewResourceFromRequest(r)
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
    if resource != nil {
        return nil, rst.NotFound()
    }

    if r.Header.Get("Content-Type") != "application/www-form-urlencoded" {
    	return nil, rst.UnsupportedMediaType("application/www-form-urlencoded")
    }

    // Detect any writing conflicts
    if rst.Conflicts(resource, r) {
		return nil, rst.PreconditionFailed()
    }

    // Read r.Body and an apply changes to resource
    // then return it
    return resource, nil
}
```

#### <a id="putter"></a>Putter

Putter allows an endpoint to handle `PUT` requests.

```go
func (ep *endpoint) Put(vars rst.RouteVars, r *http.Request) (rst.Resource, error) {
    resource := database.Find(vars.Get("id"))
    if resource != nil {
        return nil, rst.NotFound()
    }

    // Detect any writing conflicts
    if rst.Conflicts(resource, r) {
		return nil, rst.PreconditionFailed()
    }

    // Read r.Body and an apply changes to resource
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

Preflighter allows you to customize the CORS headers that will be returned to an `OPTIONS` preflight request sent by user agents before the actual request is made.

For the endpoint in this example, different policies are implemented for different times of the day.

```go
func (e *endpoint) Preflight(req *rst.AccessControlRequest, r *http.Request) *rst.AccessControlResponse {
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

// Count returns the total number of range units available
func (d *Doc) Count() uint64 {
	return uint64(len(d))
}

func (d *Doc) Range(rg *rst.Range) (*rst.ContentRange, rst.Resource, error) {
	if rg.Unit != "bytes" {
		// the Range header is ignored if the range unit passed is not bytes.
		// Request will be processed like a normal HTTP Get request because
		// ErrUnsupportedRangeUnit is returned.
		return nil, nil, ErrUnsupportedRangeUnit
	}
	cr := &ContentRange{rg, c.Count()}
	part := d[rg.From : rg.To+1]
	return cr, part, nil
}
```

#### <a id="marshaler"></a>Marshaler

Marshaler allows you to control the encoding of a resource and return the array of bytes that will form the payload of the response.

`MarshalREST` is to `rst.Marshal` what `MarshalJSON` is to `json.Marshal`.

```go
const png = "image/png"

type User struct{}
// assuming User implements rst.Resource

// MarshalREST returns the profile picture of the user if the Accept header
// of the request indicates "image/png", and relies on the rest.Marshal
// method to handle the other cases.
func (u *User) MarshalREST(r *http.Request) (string, []byte, error) {
	accept := ParseAccept(r.Header.Get("Accept"))
	if accept.Negotiate(png) == png {
		b, err := ioutil.ReadFile("path/of/user/profile/picture.png")
		return png, b, err
	}
	return rest.Marshal(rest.Resource(u), r)
}
```
