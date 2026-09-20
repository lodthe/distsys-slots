package app

import (
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestVersionedAssetsAndUncachedHTML(t *testing.T) {
	handler := NewServer(nil, Config{}).Handler()
	request := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest("GET", path, nil))
		return r
	}
	index := request("/")
	if index.Code != 200 || index.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(index.Code, index.Header())
	}
	assets := regexp.MustCompile(`/(assets/[a-f0-9]{24}/(app.js|style.css))`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(assets) != 2 {
		t.Fatal("expected fingerprinted entry script and stylesheet", index.Body.String())
	}
	for _, a := range assets {
		versioned, plain := request(a[0]), request("/"+a[2])
		if versioned.Code != 200 || versioned.Body.String() != plain.Body.String() || !strings.Contains(versioned.Header().Get("Cache-Control"), "immutable") || plain.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(a[0], versioned.Code, versioned.Header(), plain.Header())
		}
	}
	for _, p := range []string{"/assets/wrong/app.js", "/index.html", "/assets/secret/.env", "/package.json"} {
		if request(p).Code != 404 {
			t.Fatal("unexpected static path", p)
		}
	}
}
