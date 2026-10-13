package web

import (
	"bytes"
	"compress/gzip"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// Nothing in front of the app compresses responses (Fly's proxy doesn't),
// and the app shell is ~300KB of JS/CSS that gzips to ~80KB — plus the icon
// search list, 540KB raw. The embedded files never change while the binary
// runs, so each is compressed once, on first request, and kept.
var gzipCache sync.Map // name -> []byte (nil: not an embedded file)

// Images and fonts are already compressed; only text formats are worth it.
var gzipExts = map[string]bool{".js": true, ".css": true, ".json": true, ".html": true, ".svg": true}

// GzipStatic serves embedded text assets gzipped to clients that accept it,
// and passes everything else (other types, Range requests, files that don't
// exist) through to next unchanged.
func GzipStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		ext := path.Ext(name)
		if !gzipExts[ext] {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Add("Vary", "Accept-Encoding")
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) ||
			!strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Header.Get("Range") != "" {
			next.ServeHTTP(w, r)
			return
		}
		body := gzipped(name)
		if body == nil {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", mime.TypeByExtension(ext))
		h.Set("Content-Encoding", "gzip")
		h.Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(body)
	})
}

func gzipped(name string) []byte {
	if v, ok := gzipCache.Load(name); ok {
		b, _ := v.([]byte)
		return b
	}
	var out []byte
	if raw, err := FS.ReadFile(name); err == nil {
		var buf bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
		_, _ = zw.Write(raw)
		_ = zw.Close()
		out = buf.Bytes()
	}
	gzipCache.Store(name, out)
	return out
}
