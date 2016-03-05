package webmock

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Test is the interface that webmock uses to mark tests as failed, e.g. testing.T.
type Test interface {
	Error(...interface{})
	Fatal(...interface{})
}

// A Server is a wrapper around httptest.Server that allows creating
// and verifying stubs that return predefined responses.
type Server struct {
	*httptest.Server

	// URL of the underlying httptest.Server.
	URL string

	timeout       time.Duration
	defaultStatus int
	test          Test

	handler *handler
}

// NewServer creates and starts a new server. Callers should call Close or Verify
// to shut it down.
func NewServer(t Test, options ...func(*Server)) *Server {
	s := &Server{
		test:    t,
		handler: &handler{verified: true, mu: &sync.Mutex{}, fail: t.Error},
	}

	for _, opt := range options {
		opt(s)
	}

	mux := http.NewServeMux()
	mux.Handle("/", s.handler)

	s.Server = httptest.NewServer(mux)
	s.URL = s.Server.URL

	return s
}

// Stub registers a stub that is matched by HTTP method, path and other optional
// matchers against each incoming request to the server. Stubs are matched in
// the order they were registered. The stub with the most matches will be
// selected and its configured Response will be called to generate an HTTP
// response. After a stub is matched, it won't match again.
func (s *Server) Stub(method, path string, matchers ...Matcher) *Stub {
	return s.stub(&Stub{
		method:   method,
		path:     path,
		matchers: matchers,
		response: &Response{},
	})
}

// StubMatch works like Stub but tries to compile the path parameter as a
// regular expression wich is matches against each request path. It panics when
// the regular expression is invalid.
func (s *Server) StubMatch(method, pathRegexp string, matchers ...Matcher) *Stub {
	return s.stub(&Stub{
		method:     method,
		pathRegexp: regexp.MustCompile(pathRegexp),
		matchers:   matchers,
		response:   &Response{},
	})
}

func (s *Server) stub(stub *Stub) *Stub {
	s.handler.mu.Lock()
	defer s.handler.mu.Unlock()
	s.handler.verified = false
	s.handler.stubs = append(s.handler.stubs, stub)
	return stub
}

// Verify checks if all stubs have matching requests and markes the test as
// failed if there are unmatched stubs. It optionally waits for the Timeout
// duration to elapse, to allow for late requests. Call Close if matching stubs
// and requests is not important.
func (s *Server) Verify() {
	timeout := time.After(s.timeout)
	ticker := time.NewTicker(time.Millisecond * 250)

	for {
		select {
		case <-ticker.C:
			if s.handler.allVerified() {
				s.Close()
				return
			}

		case <-timeout:
			s.Close()
			if s.handler.allVerified() {
				return
			} else {
				s.test.Error(unmatchedStubsErr(s.handler.stubs))
				return
			}
		}
	}
}

// Timeout configures the time that a Server waits before failing a test
// when verifying that all stubs have matching requests.
func Timeout(d time.Duration) func(*Server) {
	return func(s *Server) {
		s.timeout = d
	}
}

// DefaultStatus configures a default response status for the Server.
func DefaultStatus(status int) func(*Server) {
	return func(s *Server) {
		s.handler.defaultStatus = status
	}
}

type handler struct {
	fail          func(...interface{})
	defaultStatus int
	verified      bool
	stubs         []*Stub
	mu            *sync.Mutex
}

func (h *handler) allVerified() bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.verified
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	req := Request{Request: r}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		h.fail(err)
		return
	}
	req.body = body

	stub := findMatch(&req, h.stubs)
	if stub == nil {
		h.fail(requestErr(req, h.stubs))
		return
	}
	stub.matched = true

	h.verified = true
	for _, stub := range h.stubs {
		if !stub.matched {
			h.verified = false
			break
		}
	}

	if fn := stub.response.handler; fn != nil {
		fn(w, r)
		return
	}

	for k, v := range stub.response.header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}
	status := stub.response.status
	if status == 0 {
		if h.defaultStatus != 0 {
			status = h.defaultStatus
		} else {
			status = http.StatusOK
		}
	}
	w.WriteHeader(status)
	if _, err := w.Write(stub.response.body); err != nil {
		h.fail(err)
	}
}

// A Stub represents a request accepted by the Server.
type Stub struct {
	method     string
	path       string
	pathRegexp *regexp.Regexp
	matchers   []Matcher
	matched    bool

	response *Response
}

// A Response represents the HTTP response for a stubbed request.
type Response struct {
	status  int
	body    []byte
	header  http.Header
	handler func(http.ResponseWriter, *http.Request)
}

// Body sets the body on a Response.
func (r *Response) Body(b []byte) *Response {
	r.body = b
	return r
}

// Body sets the header on a Response.
func (r *Response) Header(h http.Header) *Response {
	r.header = h
	return r
}

func (s Stub) String() string {
	var path string
	if s.pathRegexp != nil {
		path = s.pathRegexp.String()
	} else {
		path = s.path
	}

	var matchers []string
	for _, m := range s.matchers {
		matchers = append(matchers, m.String())
	}

	return fmt.Sprintf("%s %s %s", strings.ToUpper(s.method), path, strings.Join(matchers, ","))
}

// Respond sets the response status for a stub and returns its Response.
func (s *Stub) Respond(status int) *Response {
	s.response.status = status
	return s.response
}

// HandleFunc registers a handler function for the stub.
func (s *Stub) HandleFunc(fn func(http.ResponseWriter, *http.Request)) {
	s.response.handler = fn
}

func (s *Stub) matches(r Request) int {
	var score int
	if !s.methodAndPathMatch(r) {
		return -1
	}

	score++
	for _, matcher := range s.matchers {
		if ok := matcher.Match(r); ok {
			score++
		} else {
			score = -1
			break
		}
	}

	return score
}

func (s *Stub) methodAndPathMatch(r Request) bool {
	if s.method != r.Method {
		return false
	}

	if s.pathRegexp != nil {
		return s.pathRegexp.MatchString(r.URL.Path)
	}

	return s.path == r.URL.Path
}

// A Matcher can be added to a Stub to match request based on properties
// other than the HTTP method and request path.
type Matcher interface {
	Match(Request) bool
	fmt.Stringer
}

type bodyMatcher struct {
	body []byte
	re   *regexp.Regexp
}

// BodyEquals creates a matcher that checks if the body of a request
// is equal to b.
func BodyEquals(b []byte) Matcher {
	return bodyMatcher{body: b}
}

// BodyMatches compiles pattern to a regular expression
// and matches it to incoming request bodies.
func BodyMatches(pattern string) Matcher {
	return bodyMatcher{re: regexp.MustCompile(pattern)}
}

func (b bodyMatcher) Match(r Request) bool {
	if b.re != nil {
		return b.re.Match(r.body)
	}

	return bytes.Equal(b.body, r.body)
}

func (b bodyMatcher) String() string {
	var value string
	if b.re != nil {
		value = b.re.String()
	} else {
		value = string(b.body)
	}

	return fmt.Sprintf("body: %q", value)
}

// HeaderEquals creates a Matcher that checks if the given header key
// is present in the request header with the same value.
func HeaderEquals(key string, value []string) Matcher {
	return headerMatcher{key: key, value: value}
}

type headerMatcher struct {
	key   string
	value []string
}

func (h headerMatcher) Match(r Request) bool {
	v, ok := r.Header[h.key]
	return ok && reflect.DeepEqual(v, h.value)
}

func (h headerMatcher) String() string {
	return fmt.Sprintf("%s: %s", h.key, strings.Join(h.value, ","))
}

// Request is a wrapper around http.Request.
type Request struct {
	body []byte

	*http.Request
}

func (r Request) String() string {
	return fmt.Sprintf("%s %s, body: %q", r.Method, r.URL.Path, string(r.body))
}

func findMatch(r *Request, stubs []*Stub) *Stub {
	var maxScore int
	var match *Stub

	for _, stub := range stubs {
		if stub.matched {
			continue
		}

		if score := stub.matches(*r); score > maxScore {
			maxScore = score
			match = stub
		}
	}

	return match
}

func requestErr(r Request, stubs []*Stub) error {
	return fmt.Errorf("\n\nUnregistered request: %s\n\nstubbed requests: \n\n%s", r, stubList(stubs))
}

func unmatchedStubsErr(stubs []*Stub) error {
	return fmt.Errorf("\n\nNot all stubs have been matched: \n\n%s", stubList(stubs))
}

func stubList(stubs []*Stub) string {
	var list string
	for i, stub := range stubs {
		s := fmt.Sprintf("%d. %s", i+1, stub)
		if stub.matched {
			s += " (matched)"
		}
		s += "\n"
		list += s
	}
	return list
}
