package web

import (
	"io/fs"
	"strings"
	"testing"
)

// The frontend is ES modules under app/, and two hand-kept lists have to
// name every one: the service worker's PRECACHE_URLS (or the module is
// missing offline, and a cache-first shell loads a stale copy next to fresh
// ones) and index.html's modulepreload links (or it loads a round trip
// late). Nothing else would notice a module left out of either.
func TestEveryAppModuleIsPrecachedAndPreloaded(t *testing.T) {
	modules, err := fs.Glob(FS, "app/*.js")
	if err != nil || len(modules) == 0 {
		t.Fatalf("no app modules embedded: %v", err)
	}
	sw, _ := FS.ReadFile("sw.js")
	index, _ := FS.ReadFile("index.html")
	for _, m := range modules {
		if !strings.Contains(string(sw), `"/`+m+`"`) {
			t.Errorf("sw.js PRECACHE_URLS lacks /%s", m)
		}
		if m == "app/main.js" {
			if !strings.Contains(string(index), `<script type="module" src="/app/main.js">`) {
				t.Error("index.html doesn't load /app/main.js as a module")
			}
			continue
		}
		if !strings.Contains(string(index), `<link rel="modulepreload" href="/`+m+`" />`) {
			t.Errorf("index.html has no modulepreload for /%s", m)
		}
	}
}
