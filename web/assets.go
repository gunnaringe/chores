// Package web embeds the static frontend assets into the binary.
package web

import "embed"

//go:embed index.html app.js theme.js icon-probe.js boot.js login.js privacy.js app.css i18n.js login.html privacy.html manifest.webmanifest sw.js qrcode.js icons fonts screenshots material-symbols.json material-symbols-i18n.json
var FS embed.FS
