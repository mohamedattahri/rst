package rst

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func normalizeHeaderArray(headers []string) []string {
	for i, name := range headers {
		headers[i] = http.CanonicalHeaderKey(name)
	}
	return headers
}

// DefaultAccessControl defines a limited CORS policy that only allows simple
// cross-origin requests.
var DefaultAccessControl = &AccessControlResponse{
	Origin:         "*",
	Credentials:    true,
	AllowedHeaders: nil,
	ExposedHeaders: []string{"Etag"},
	Methods:        nil,
	MaxAge:         24 * time.Hour,
}

// PermissiveAccessControl defines a permissive CORS policy in which all methods
// and all headers are allowed for all origins.
var PermissiveAccessControl = &AccessControlResponse{
	Origin:         "*",
	Credentials:    true,
	AllowedHeaders: []string{},
	ExposedHeaders: []string{"Etag"},
	Methods:        []string{},
	MaxAge:         24 * time.Hour,
}

/*
Preflighter is implemented by endpoints wishing to customize the response to
a CORS preflighted request.

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
*/
type Preflighter interface {
	Preflight(*AccessControlRequest, RouteVars, *http.Request) *AccessControlResponse
}

// AccessControlRequest represents the headers of a CORS access control request.
type AccessControlRequest struct {
	Origin  string
	Method  string
	Headers []string
}

func (ac *AccessControlRequest) isEmpty() bool {
	return ac.Origin == "" && ac.Method == "" && len(ac.Headers) == 0
}

// ParseAccessControlRequest returns a new instance of AccessControlRequest
// filled with CORS headers found in r.
func ParseAccessControlRequest(r *http.Request) *AccessControlRequest {
	var headers []string
	if h := r.Header.Get("Access-Control-Request-Headers"); h != "" {
		headers = strings.Split(strings.Replace(r.Header.Get("Access-Control-Request-Headers"), " ", "", -1), ",")
	}
	return &AccessControlRequest{
		Origin:  r.Header.Get("Origin"),
		Method:  r.Header.Get("Access-Control-Request-Method"),
		Headers: headers,
	}

	// TODO: remove duplicated headers before serving them back.
}

// AccessControlResponse defines the response headers to a CORS access control
// request.
type AccessControlResponse struct {
	Origin         string
	ExposedHeaders []string
	Methods        []string // Empty array means any, nil means none.
	AllowedHeaders []string // Empty array means any, nil means none.
	Credentials    bool
	MaxAge         time.Duration
}

type accessControlHandler struct {
	endpoint Endpoint
	*AccessControlResponse
}

func newAccessControlHandler(endpoint Endpoint, ac *AccessControlResponse) *accessControlHandler {
	return &accessControlHandler{
		endpoint:              endpoint,
		AccessControlResponse: ac,
	}
}

func (h *accessControlHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if _, exists := r.Header["Origin"]; !exists {
		return
	}

	req := ParseAccessControlRequest(r)

	var resp *AccessControlResponse
	if h.endpoint == nil {
		resp = h.AccessControlResponse
	} else {
		if preflighter, implemented := h.endpoint.(Preflighter); implemented && strings.ToUpper(r.Method) == Options {
			// If Options and endpoint implements Preflighter, call Preflight.
			resp = preflighter.Preflight(req, getVars(r), r)
		} else {
			resp = h.AccessControlResponse
		}
	}

	// Adding a vary if an origin is specified in the response.
	defer func() {
		if allowed := w.Header().Get("Access-Control-Allow-Origin"); allowed != "" && allowed != "*" {
			addVary(w.Header(), "Origin")
		}
	}()

	// Writing response headers
	if resp.Origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", resp.Origin)
	}
	w.Header().Set("Access-Control-Allow-Credentials", strconv.FormatBool(resp.Credentials))

	// Exposed headers
	if len(resp.ExposedHeaders) > 0 {
		w.Header().Set("Access-Control-Expose-Headers", strings.Join(normalizeHeaderArray(resp.ExposedHeaders), ", "))
	}

	// OPTIONS only
	if strings.ToUpper(r.Method) != Options {
		return
	}

	w.Header().Set("Access-Control-Max-Age", strconv.Itoa(int(resp.MaxAge.Seconds())))

	if req.Method != "" && resp.Methods != nil {
		var methods []string
		if len(resp.Methods) == 0 {
			methods = AllowedMethods(h.endpoint)
		} else {
			methods = resp.Methods
		}
		w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	}

	if len(req.Headers) > 0 && resp.AllowedHeaders != nil {
		var headers []string
		if len(resp.AllowedHeaders) == 0 {
			headers = req.Headers
		} else {
			headers = resp.AllowedHeaders
		}
		w.Header().Set("Access-Control-Allow-Headers", strings.Join(normalizeHeaderArray(headers), ", "))
	}
}
