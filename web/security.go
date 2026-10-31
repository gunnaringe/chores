package web

import "net/http"

// contentSecurityPolicy is the backstop behind escapeHtml(): the whole UI
// is built from template strings, so one forgotten escape is an XSS hole,
// and this keeps such a hole from running script. It holds only because
// no page has inline <script> blocks or on*= handler attributes — page
// scripts live in their own files (head.js, login.js, privacy.js, boot.js)
// and the app (app/*.js) wires events with addEventListener. Inline *styles* stay
// allowed: the templates use style= attributes throughout, and styles
// can't execute anything.
//
// data: images are the QR codes qrcode.js draws; blob: covers images the
// app builds client-side.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"manifest-src 'self'; " +
	"worker-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'"

// SecurityHeaders sets the response headers every page and API call gets.
//
// Framing is refused everywhere except /dashboard: the kiosk view is
// exactly the kind of page someone puts in a Home Assistant iframe panel,
// and all it can do is tick off chores, so there is nothing there worth
// clickjacking. Everything else — Settings, payouts, invitations — can't
// be framed at all.
//
// HSTS is only sent over HTTPS (directly or via a proxy's
// X-Forwarded-Proto), and without includeSubDomains, since a self-hoster's
// sibling hosts are none of this app's business.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		frameAncestors := "'none'"
		if r.URL.Path == "/dashboard" {
			frameAncestors = "*"
		} else {
			h.Set("X-Frame-Options", "DENY")
		}
		h.Set("Content-Security-Policy", contentSecurityPolicy+"; frame-ancestors "+frameAncestors)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
