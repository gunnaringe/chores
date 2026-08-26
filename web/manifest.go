package web

import (
	"encoding/json"
	"net/http"
)

// appNames maps a UI language to the name an installed PWA gets. These are
// the same two names as the "app.name" key in web/i18n.js, which is what the
// running UI uses — keep the two in step. English is the fallback for any
// language this doesn't know, matching t()'s own fallback.
var appNames = map[string]string{
	"en": "Chores",
	"nb": "Ukelønn",
}

// ManifestHandler serves the web app manifest with its name localized to the
// ?lang= query parameter. It can't be a static file: the installed app's name
// has to follow the language the user picked in the UI, and that choice lives
// in localStorage, so the page rewrites the <link rel="manifest"> href to
// carry it (see applyAppName in web/i18n.js).
func ManifestHandler(w http.ResponseWriter, r *http.Request) {
	raw, err := FS.ReadFile("manifest.webmanifest")
	if err != nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	// Decoded and re-encoded rather than string-substituted so the embedded
	// file stays the single definition of everything else in the manifest.
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	name, ok := appNames[r.URL.Query().Get("lang")]
	if !ok {
		name = appNames["en"]
	}
	manifest["name"] = name
	manifest["short_name"] = name

	body, err := json.Marshal(manifest)
	if err != nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	w.Header().Set("Content-Type", "application/manifest+json")
	// The name varies by query string, so a cache keyed on path alone would
	// hand a Norwegian install the English manifest.
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(body)
}
