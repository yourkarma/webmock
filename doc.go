/*
Package webmock provides a way to mock HTTP requests and responses.

	func TestEndpoint(t *testing.T) {
		s := webmock.NewServer(t)
		s.Stub("GET", "/hello").Respond(200).Body([]byte("Hello, World"))
		defer s.Verify()

		resp, err := http.Get(s.URL + "/hello")
		if err != nil {
			t.Fatal(err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

*/
package webmock
