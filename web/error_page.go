package web

import (
	"html"
	"net/http"
	"strings"
)

// RenderErrorPage writes a small HTML page styled like the rest of the app,
// for the handful of places a browser can land directly on a plain-text
// failure — an expired invite link, a broken OAuth callback — rather than
// an API error the frontend already renders inline itself.
func RenderErrorPage(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(strings.Replace(errorPageTemplate, "{{MESSAGE}}", html.EscapeString(message), 1)))
}

const errorPageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover" />
<title>Chores</title>
<link rel="stylesheet" href="/app.css" />
<link rel="icon" href="/icons/icon-192.png" />
<link rel="apple-touch-icon" href="/icons/apple-touch-icon.png" />
<meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)" />
<meta name="theme-color" content="#101216" media="(prefers-color-scheme: dark)" />
<meta name="color-scheme" content="light dark" />
</head>
<body>
<div id="app">
  <main class="content no-tabbar">
    <div class="hero">
      <img src="/icons/logo.png" alt="" width="88" height="88" class="logo" />
      <h1>Something went wrong</h1>
      <p>{{MESSAGE}}</p>
      <button type="button" class="block" onclick="location.href='/'">Back to Chores</button>
    </div>
  </main>
</div>
</body>
</html>
`
