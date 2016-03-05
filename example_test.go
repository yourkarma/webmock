package webmock_test

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/yourkarma/webmock"
)

type TestingT struct{}

func (t TestingT) Error(args ...interface{}) {
	log.Print(args...)
}

func (t TestingT) Fatal(args ...interface{}) {
	log.Fatal(args...)
}

func ExampleServer() {
	s := webmock.NewServer(TestingT{})
	s.Stub("GET", "/hello").Respond(200).Body([]byte("Hello, World"))
	defer s.Verify()

	resp, err := http.Get(s.URL + "/hello")
	if err != nil {
		log.Fatal(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s", body)
	// Output: Hello, World
}
