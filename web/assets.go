// Package web embeds the static frontend assets into the binary.
package web

import "embed"

//go:embed index.html app.js app.css i18n.js login.html manifest.webmanifest sw.js qrcode.js icons screenshots material-symbols.json
var FS embed.FS
