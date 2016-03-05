package webmock_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/yourkarma/webmock"
)

func TestStub(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()
	s.Stub("GET", "/")

	if _, err := http.Get(s.URL); err != nil {
		t.Fatal(err)
	}
}

func TestStubMatch(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()
	s.StubMatch("GET", "/users/\\d+")

	if _, err := http.Get(s.URL + "/users/123"); err != nil {
		t.Fatal(err)
	}
}

func TestBodyEquals(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()

	s.Stub("POST", "/", webmock.BodyEquals([]byte("name=Bob")))
	if _, err := http.PostForm(s.URL, url.Values{"name": {"Bob"}}); err != nil {
		t.Fatal(err)
	}
}

func TestBodyMatches(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()
	s.Stub("POST", "/", webmock.BodyMatches("name=\\S+"))

	if _, err := http.PostForm(s.URL, url.Values{"name": {"Bob"}}); err != nil {
		t.Fatal(err)
	}
}

func TestBestMatchWins(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Close()

	s.Stub("POST", "/users/1").Respond(500)
	s.Stub("POST", "/users/1", webmock.BodyEquals([]byte("name=Bob"))).Respond(200)
	resp, err := http.PostForm(s.URL+"/users/1", url.Values{"name": {"Bob"}})
	if err != nil {
		t.Fatal(err)
	}

	if resp.StatusCode != 200 {
		t.Fatal("expected different match")
	}
}

func TestRespStatus(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Close()

	expectStatus := 404
	s.Stub("GET", "/users/1").Respond(404)

	resp, err := http.Get(s.URL + "/users/1")
	if err != nil {
		t.Fatal(err)
	}

	if got := resp.StatusCode; got != expectStatus {
		t.Fatalf("expected status code %d, got %d", expectStatus, got)
	}
}

func TestStatusDefaultsToOK(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Close()

	s.Stub("GET", "/users/1")

	resp, err := http.Get(s.URL + "/users/1")
	if err != nil {
		t.Fatal(err)
	}

	expectStatus := 200
	if got := resp.StatusCode; got != expectStatus {
		t.Fatalf("expected status code %d, got %d", expectStatus, got)
	}
}

func TestResponseBody(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Close()

	expectBody := []byte("Bob")
	s.Stub("GET", "/users/1").Respond(200).Body(expectBody)

	resp, err := http.Get(s.URL + "/users/1")
	if err != nil {
		t.Fatal(err)
	}

	got, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, expectBody) {
		t.Fatalf("expected response body %q, got %q", expectBody, got)
	}
}

func TestNoExpectationsVerify(t *testing.T) {
	test := &fakeTest{}
	s := webmock.NewServer(test)
	s.Verify()
	if test.err != nil {
		t.Fatal("Expected test not to be failed, but got err: %s", test.err)
	}
}

func TestUnmockedRequest(t *testing.T) {
	test := &fakeTest{}
	s := webmock.NewServer(test)
	defer s.Close()

	s.Stub("GET", "/foo").Respond(200)
	s.Stub("POST", "/bar",
		webmock.BodyEquals([]byte("greeting=hello")),
	).Respond(200)

	http.PostForm(s.URL+"/bar", url.Values{"greeting": {"hello"}})
	http.Get(s.URL + "/unknown-path")

	actual := test.err.Error()
	expect := []string{
		`Unregistered request: GET /unknown-path, body: ""`,
		"1. GET /foo",
		`2. POST /bar body: "greeting=hello" (matched)`,
	}
	for _, exp := range expect {
		if strings.Index(actual, exp) == -1 {
			t.Fatalf("Expected %q in %q", exp, actual)
		}
	}
}

func TestVerifyStubs(t *testing.T) {
	test := &fakeTest{}
	s := webmock.NewServer(test)

	s.Stub("GET", "/foo").Respond(200)
	s.Verify()

	actual := test.err.Error()
	expect := []string{
		"Not all stubs have been matched",
		"1. GET /foo",
	}
	for _, exp := range expect {
		if strings.Index(actual, exp) == -1 {
			t.Fatal("Expected %q in %q", exp, actual)
		}
	}
}

func TestResponseHandler(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()

	stubResponse := "hello"
	s.Stub("GET", "/").HandleFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte(stubResponse)); err != nil {
			t.Fatal(err)
		}
	})

	resp, err := http.Get(s.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	if bs := string(b); bs != stubResponse {
		t.Fatal("expected response %q, got %q", stubResponse, bs)
	}
}

func TestHeaderEquals(t *testing.T) {
	s := webmock.NewServer(t)
	defer s.Verify()

	s.Stub("GET", "/",
		webmock.HeaderEquals("Content-Type", []string{"application/json"}),
	).Respond(200)
	req, err := http.NewRequest("GET", s.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Add("Content-Type", "application/json")
	if _, err := http.DefaultClient.Do(req); err != nil {
		t.Fatal(err)
	}
}

type fakeTest struct {
	err error
}

func (f *fakeTest) Error(args ...interface{}) {
	f.err = args[0].(error)
}

func (f *fakeTest) Fatal(args ...interface{}) {
	f.err = args[0].(error)
}
