package web

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

var (
	materialSymbolsOnce sync.Once
	materialSymbolsRaw  []byte
	materialSymbolsGzip []byte
)

// MaterialSymbolsHandler serves material-symbols.json, gzipped when the
// client accepts it. Nothing else in front of the app compresses responses
// (Fly's proxy doesn't), and with Google's search tags in it the icon list
// is ~540KB raw but ~180KB gzipped — worth a dedicated handler, since the
// task icon picker fetches it on every fresh load of the task form.
// Compressed once, on first request.
func MaterialSymbolsHandler(w http.ResponseWriter, r *http.Request) {
	materialSymbolsOnce.Do(func() {
		raw, err := FS.ReadFile("material-symbols.json")
		if err != nil {
			return
		}
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		materialSymbolsRaw, materialSymbolsGzip = raw, buf.Bytes()
	})
	if materialSymbolsRaw == nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Vary", "Accept-Encoding")
	// Changes only with a release, like the fonts (see notFoundPage in
	// cmd/chores/main.go).
	h.Set("Cache-Control", "public, max-age=604800")
	body := materialSymbolsRaw
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = materialSymbolsGzip
	}
	_, _ = w.Write(body)
}
