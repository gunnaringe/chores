package web

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"strings"
)

// langPlaceholder is the opening tag both documents ship with. Render
// rewrites it so the served page states the language the hostname implies —
// see Pages.
const langPlaceholder = `<html lang="en">`

// ogPlaceholder is the comment in index.html and login.html that the
// rendered <meta> block replaces. A comment rather than a template action
// ({{...}}) so that the raw files stay valid HTML: the static file server
// hands them out untouched at /index.html and /login.html, and a stray
// placeholder there should be invisible rather than printed.
const ogPlaceholder = "<!-- og:meta -->"

// ogPages are the two documents rendered per request: the welcome page an
// unauthenticated visit to "/" gets, and the app shell behind /dashboard.
var ogPages = []string{"index.html", "login.html"}

// ogTaglines is the one-line description a link preview shows, per UI
// language. These are the "login.tagline" strings from web/i18n.js — the
// same sentence the page itself displays under its title — duplicated here
// because a crawler never runs i18n.js and so only ever sees what is
// literally in the served HTML. Keep them in step. English is the fallback
// for any language this doesn't know, matching t()'s own fallback and
// appNames in manifest.go, whose localized app names supply the title.
var ogTaglines = map[string]string{
	"en": "Family chores and allowance, in one place.",
	"nb": "Husarbeid og ukepenger på ett sted.",
	"nn": "Husarbeid og vekepengar på éin stad.",
	"sv": "Hushållssysslor och veckopeng, på ett ställe.",
}

// ogLocales maps a UI language to the underscored locale code Facebook
// expects in og:locale, which is not the same shape as the bare language
// tag used everywhere else in this app.
var ogLocales = map[string]string{
	"en": "en_US",
	"nb": "nb_NO",
	"nn": "nn_NO",
	"sv": "sv_SE",
}

// ogTemplate is html/template rather than string concatenation because
// .Origin is built from the request's own Host header, which is caller-
// controlled: pasted in raw it would be an HTML injection into every
// attribute below. The template's attribute-context escaping is what makes
// reflecting it safe.
var ogTemplate = template.Must(template.New("og").Parse(
	`<meta name="description" content="{{.Description}}" />
<meta property="og:title" content="{{.Title}}" />
<meta property="og:description" content="{{.Description}}" />
<meta property="og:type" content="website" />
<meta property="og:url" content="{{.Origin}}/" />
<meta property="og:site_name" content="{{.Title}}" />
<meta property="og:locale" content="{{.Locale}}" />
<meta property="og:image" content="{{.Origin}}/icons/og-image-{{.Lang}}.png" />
<meta property="og:image:type" content="image/png" />
<meta property="og:image:width" content="1200" />
<meta property="og:image:height" content="630" />
<meta property="og:image:alt" content="{{.Title}} — {{.Description}}" />
<meta name="twitter:card" content="summary_large_image" />`))

type ogData struct {
	Title       string
	Description string
	Locale      string
	Lang        string
	Origin      string
}

// Pages serves the two HTML documents whose content depends on which
// hostname was asked for: a deployment can answer to several names, and a
// name like "ukepenger.example" is itself a statement about what language
// the visitor expects.
//
// Two things follow from that hostname. The link-preview (og:) tags, which
// a crawler reads without ever running i18n.js, and the page's default UI
// language, which the served <html lang> carries into getLang() — see
// Render. Neither can be a static file, for the same reason the manifest
// can't be.
type Pages struct {
	// hostLangs maps a lowercased hostname to the language it defaults to.
	// Deliberately configuration rather than a constant: no deployment's
	// hostnames belong in this repo's source, and the same binary has to
	// work behind any name pointed at it.
	hostLangs map[string]string
}

// NewPages fails rather than returning a Pages that would silently serve
// previews with a visible "<!-- og:meta -->" and no tags, which is exactly
// the failure that is invisible in a browser and only shows up once someone
// pastes a link into Facebook.
func NewPages(hostLangs map[string]string) (*Pages, error) {
	for _, name := range ogPages {
		raw, err := FS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		for _, placeholder := range []string{ogPlaceholder, langPlaceholder} {
			if !bytes.Contains(raw, []byte(placeholder)) {
				return nil, fmt.Errorf("%s is missing the %s placeholder", name, placeholder)
			}
		}
	}
	return &Pages{hostLangs: hostLangs}, nil
}

// ParseHostLanguages reads the host-to-language mapping that decides which
// language a hostname defaults to, in the form
//
//	ukepenger.apphub.casa=nb,vekepengar.apphub.casa=nn
//
// An empty string maps nothing, leaving every hostname on the existing
// behaviour: English previews, and a UI that follows the browser. Anything
// malformed — or naming a language the app doesn't have — is an error
// rather than a silently dropped entry, since the symptom otherwise only
// appears in someone else's Facebook feed.
func ParseHostLanguages(spec string) (map[string]string, error) {
	hosts := map[string]string{}
	for _, pair := range strings.Split(spec, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		host, lang, ok := strings.Cut(pair, "=")
		host, lang = strings.ToLower(strings.TrimSpace(host)), strings.TrimSpace(lang)
		if !ok || host == "" || lang == "" {
			return nil, fmt.Errorf("%q is not a host=lang pair", pair)
		}
		if _, known := appNames[lang]; !known {
			return nil, fmt.Errorf("unknown language %q for host %q", lang, host)
		}
		hosts[host] = lang
	}
	return hosts, nil
}

// language picks the language a request defaults to. An explicit ?lang=
// wins — it's how ManifestHandler is already steered, and it makes a
// preview testable without touching DNS — then the configured hostname
// mapping, then English.
//
// A visitor's own stored choice outranks all of this, but that lives in
// localStorage and so can only be applied in the browser: getLang() in
// i18n.js checks it before falling back to what this stamped out.
func (p *Pages) language(r *http.Request) string {
	if lang := r.URL.Query().Get("lang"); lang != "" {
		if _, known := appNames[lang]; known {
			return lang
		}
	}
	host := strings.ToLower(r.Host)
	// Host carries a port for a local dev server but never in production.
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if lang, ok := p.hostLangs[host]; ok {
		return lang
	}
	return "en"
}

// origin reconstructs the absolute scheme://host this request came in on.
// og:url and og:image have to be absolute — Facebook ignores relative ones
// — but hardcoding a base URL would break every other hostname pointed at
// the deployment, so it is derived from the request for the same reason
// redirectURI in internal/auth is (see the comment there).
//
// Each hostname must also advertise itself rather than a shared canonical
// URL: Facebook caches a scrape against the og:url it is told, so pointing
// two domains at one canonical would show the first one's card for both.
func origin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// Render serves one of the ogPages with its link-preview metadata and
// default language filled in for this request.
func (p *Pages) Render(w http.ResponseWriter, r *http.Request, name string) {
	raw, err := FS.ReadFile(name)
	if err != nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	lang := p.language(r)
	title, ok := appNames[lang]
	if !ok {
		title = appNames["en"]
	}
	tagline, ok := ogTaglines[lang]
	if !ok {
		tagline = ogTaglines["en"]
	}

	var block bytes.Buffer
	if err := ogTemplate.Execute(&block, ogData{
		Title:       title,
		Description: tagline,
		Locale:      ogLocales[lang],
		Lang:        lang,
		Origin:      origin(r),
	}); err != nil {
		RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}

	body := bytes.Replace(raw, []byte(ogPlaceholder), block.Bytes(), 1)

	// Both attributes are set from the same value, but they say different
	// things: lang is what the document is actually in, for screen readers
	// and search engines, while data-default-lang is the fallback getLang()
	// reads and can still be overridden by a stored preference. lang is a
	// key of appNames, never free text, so it needs no escaping here.
	body = bytes.Replace(body, []byte(langPlaceholder),
		[]byte(`<html lang="`+lang+`" data-default-lang="`+lang+`">`), 1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The tags vary by hostname and by ?lang=, so a cache keyed on path
	// alone would hand the Norwegian domain the English preview.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Vary", "Host")
	_, _ = w.Write(body)
}
