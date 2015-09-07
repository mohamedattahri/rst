// Copyright (c) 2014, Mohamed Attahri
// Copyright (c) 2011, Open Knowledge Foundation Ltd.

package rst

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// addVary adds value to the list of values of the "Vary" header if it's not
// already there.
func addVary(header http.Header, value string) {
	value = http.CanonicalHeaderKey(value)
	if values, ok := header[http.CanonicalHeaderKey("Vary")]; ok {
		for _, v := range values {
			if v == value {
				return
			}
		}
	}
	header.Add("Vary", value)
}

// AcceptClause represents a clause in an HTTP Accept header.
type AcceptClause struct {
	Type, SubType string
	Q             float64
	Params        map[string]string
}

// Accept represents a set of clauses in an HTTP Accept header.
type Accept []AcceptClause

func (accept Accept) Len() int {
	return len(accept)
}

func (accept Accept) Less(i, j int) bool {
	ai, aj := accept[i], accept[j]
	if ai.Q > aj.Q {
		return true
	}
	if ai.Type != "*" && aj.Type == "*" {
		return true
	}
	if ai.SubType != "*" && aj.SubType == "*" {
		return true
	}
	return false
}

func (accept Accept) Swap(i, j int) {
	accept[i], accept[j] = accept[j], accept[i]
}

// ParseAccept parses the raw value of an accept Header, and returns a sorted
// list of clauses.
func ParseAccept(header string) Accept {
	accept := make(Accept, 0)
	for _, part := range strings.Split(header, ",") {
		part := strings.Trim(part, " ")

		a := AcceptClause{}
		a.Params = make(map[string]string)
		a.Q = 1.0

		mrp := strings.Split(part, ";")

		mediaRange := mrp[0]
		sp := strings.Split(mediaRange, "/")
		a.Type = strings.Trim(sp[0], " ")

		switch {
		case len(sp) == 1 && a.Type == "*":
			a.SubType = "*"
		case len(sp) == 2:
			a.SubType = strings.Trim(sp[1], " ")
		default:
			continue
		}

		if len(mrp) == 1 {
			accept = append(accept, a)
			continue
		}

		for _, param := range mrp[1:] {
			sp := strings.SplitN(param, "=", 2)
			if len(sp) != 2 {
				continue
			}
			token := strings.Trim(sp[0], " ")
			if token == "q" {
				a.Q, _ = strconv.ParseFloat(sp[1], 32)
			} else {
				a.Params[token] = strings.Trim(sp[1], " ")
			}
		}

		accept = append(accept, a)
	}

	sort.Sort(accept)
	return accept
}

// Negotiate the most appropriate contentType given the accept header clauses
// and a list of alternatives.
func (accept Accept) Negotiate(alternatives ...string) (contentType string) {
	asp := make([][]string, 0, len(alternatives))
	for _, ctype := range alternatives {
		asp = append(asp, strings.SplitN(ctype, "/", 2))
	}
	for _, clause := range accept {
		for i, ctsp := range asp {
			if clause.Type == ctsp[0] && clause.SubType == ctsp[1] {
				contentType = alternatives[i]
				return
			}
			if clause.Type == ctsp[0] && clause.SubType == "*" {
				contentType = alternatives[i]
				return
			}
			if clause.Type == "*" && clause.SubType == "*" {
				contentType = alternatives[i]
				return
			}
		}
	}
	return
}

var (
	rangeRe = regexp.MustCompile("^(\\w+)=(\\d+)-(\\d+)?$")
)

// Range is a structured representation of the Range request header.
//
type Range struct {
	Unit string
	From uint64
	To   uint64
}

// Len returns the number of units requested in this range.
func (r *Range) Len() uint64 {
	return r.To - r.From
}

// validate the range for ranger.
func (r *Range) validate(ranger Ranger) error {
	for _, u := range ranger.Units() {
		if strings.EqualFold(r.Unit, u) {
			return nil
		}
	}
	return fmt.Errorf("unsupported range unit %s", r.Unit)
}

/*
adjust will correct r to fall within the boundaries of ranger. If r does not
overlap the current extend of ranger, a RequestedRangeNotSatifiable error will
be returned.

Range entities are always adjusted before they are passed to Ranger.Range
implementer.
*/
func (r *Range) adjust(ranger Ranger) error {

	count := ranger.Count()
	if r.From > count {
		return RequestedRangeNotSatisfiable(&ContentRange{Total: count})
	}
	r.To = uint64(math.Min(float64(r.To), float64(count-1)))
	return nil
}

/*
ParseRange parses raw into a new Range instance.

	ParseRange("bytes=0-1024") 	// (OK)
	ParseRange("resources=239-392")	// (OK)
	ParseRange("items=39-")		// (OK)
	ParseRange("bytes 50-100")	// (ERROR: syntax)
	ParseRange("bytes=100-50")	// (ERROR: logic)
*/
func ParseRange(raw string) (*Range, error) {
	m := rangeRe.FindStringSubmatch(raw)
	if m == nil || len(m) < 4 {
		return nil, errors.New("malformed Range header value")
	}

	r := &Range{
		Unit: m[1],
	}

	// Regex guarantees numbers are valid, so errors of strconv.ParseUint can
	// be safely ignored.

	r.From, _ = strconv.ParseUint(m[2], 10, 64)

	// To is optional. When omitted, it means "all remaining available units".
	if m[3] != "" {
		r.To, _ = strconv.ParseUint(m[3], 10, 64)
		if r.From > r.To {
			return nil, errors.New("invalid Range header value")
		}
	} else {
		r.To = math.MaxUint64
	}

	return r, nil
}

// ContentRange is a structured representation of the Content-Range response
// header.
type ContentRange struct {
	*Range
	Total uint64
}

func (cr *ContentRange) String() string {
	if cr.Total == 0 {
		return "*/*"
	}

	if cr.Range == nil {
		return fmt.Sprintf("*/%d", cr.Total)
	}

	return fmt.Sprintf("%s %d-%d/%d", cr.Unit, cr.From, cr.To, cr.Total)
}
