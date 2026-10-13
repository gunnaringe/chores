package web

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaterialSymbolsHandler(t *testing.T) {
	for _, gz := range []bool{false, true} {
		req := httptest.NewRequest("GET", "/material-symbols.json", nil)
		if gz {
			req.Header.Set("Accept-Encoding", "gzip, deflate, br")
		}
		rec := httptest.NewRecorder()
		MaterialSymbolsHandler(rec, req)

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
		found := false
		for _, e := range entries {
			if strings.HasPrefix(e, "delete ") && strings.Contains(e, " trash") {
				found = true
			}
		}
		if !found {
			t.Fatalf("gzip=%v: no \"delete\" entry tagged \"trash\" among %d entries", gz, len(entries))
		}
	}
}
