// Package web embeds the static frontend assets into the binary.
package web

import "embed"

//go:embed index.html app.js app.css login.html manifest.webmanifest sw.js icons
var FS embed.FS
