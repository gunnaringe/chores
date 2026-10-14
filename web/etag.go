package web

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
)

var etags sync.Map // name -> string ("" if not an embedded file)

// ETag returns a strong ETag for an embedded file, derived from its
// content: embedded files have no modification time, so this is the only
// way a browser or Cloudflare can revalidate one with a cheap 304 instead
// of downloading it again. Computed once per file.
func ETag(name string) string {
	if v, ok := etags.Load(name); ok {
		return v.(string)
	}
	tag := ""
	if raw, err := FS.ReadFile(name); err == nil {
		sum := sha256.Sum256(raw)
		tag = `"` + hex.EncodeToString(sum[:8]) + `"`
	}
	etags.Store(name, tag)
	return tag
}
