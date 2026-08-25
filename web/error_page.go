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
<meta name="viewport" content="width=device-width, initial-scale=1" />
<title>Chores</title>
<link rel="stylesheet" href="/app.css" />
<link rel="icon" href="/icons/icon-192.png" />
</head>
<body>
<div id="app">
  <div class="card" style="max-width:360px;margin:12vh auto 0;text-align:center;">
    <img src="/icons/logo.png" alt="" width="96" height="96" class="logo" />
    <h1>Something went wrong</h1>
    <p>{{MESSAGE}}</p>
    <button type="button" onclick="location.href='/'">Back to Chores</button>
  </div>
</div>
</body>
</html>
`
