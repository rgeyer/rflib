package web

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

// Without a build the handler explains; with one, any app route gets
// index.html and assets are served as files.
func TestHandler(t *testing.T) {
	get := func(path string) (int, string, string) {
		rec := httptest.NewRecorder()
		Handler().ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		body, _ := io.ReadAll(rec.Body)
		return rec.Code, string(body), rec.Header().Get("Cache-Control")
	}
	if !Built() {
		if code, body, _ := get("/l/default/channels"); code != 200 || !strings.Contains(body, "make web") {
			t.Errorf("placeholder = %d %q", code, body)
		}
		return
	}
	code, body, cache := get("/l/default/radios/HT")
	if code != 200 || !strings.Contains(body, `<div id="root">`) || cache != "no-cache" {
		t.Errorf("app route = %d %q %q", code, cache, body)
	}
}
