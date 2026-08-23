// Package web embeds the static frontend assets into the binary.
package web

import "embed"

//go:embed index.html app.js app.css login.html
var FS embed.FS
