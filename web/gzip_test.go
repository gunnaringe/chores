package web

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipStatic(t *testing.T) {
	h := GzipStatic(http.FileServerFS(FS))
	get := func(path string, gz bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		if gz {
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// The icon search list, both ways, decodes to the same JSON.
	for _, gz := range []bool{false, true} {
		rec := get("/material-symbols.json", gz)
		var body io.Reader = rec.Body
		if gz {
			if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
				t.Fatalf("Content-Encoding = %q, want gzip", got)
			}
			zr, err := gzip.NewReader(rec.Body)
			if err != nil {
				t.Fatal(err)
			}
			body = zr
		} else if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("Content-Encoding = %q without Accept-Encoding", got)
		}
		var entries []string
		if err := json.NewDecoder(body).Decode(&entries); err != nil {
			t.Fatalf("gzip=%v: %v", gz, err)
		}
		if len(entries) < 1000 {
			t.Fatalf("gzip=%v: only %d entries", gz, len(entries))
		}
	}

	rec := get("/app.js", true)
	if rec.Header().Get("Content-Encoding") != "gzip" || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("app.js: encoding %q, type %q", rec.Header().Get("Content-Encoding"), rec.Header().Get("Content-Type"))
	}
	// Already-compressed formats and missing files pass through untouched.
	if rec := get("/icons/icon-192.png", true); rec.Header().Get("Content-Encoding") != "" || rec.Code != 200 {
		t.Fatalf("png: code %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
	if rec := get("/nope.js", true); rec.Code != 404 || rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("missing file: code %d, encoding %q", rec.Code, rec.Header().Get("Content-Encoding"))
	}
}
