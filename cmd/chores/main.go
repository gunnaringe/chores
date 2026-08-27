// Command chores runs Chores, a family allowance and chore tracker: a
// single binary serving both the embedded web UI and the Connect API,
// backed by a local SQLite database.
package main

import (
	"context"
	"errors"
	"flag"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/basicflag"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/db"
	"github.com/gunnaringe/chores/internal/server"
	"github.com/gunnaringe/chores/web"
)

// loadConfig layers config sources with koanf, lowest to highest priority:
// an optional .env file, real environment variables, then CLI flags on
// top. Each layer only overrides the one below it for values it actually
// sets — an unset flag never clobbers a value an earlier layer provided
// (see basicflag's Opt{KeyMap: k}) — which is what lets AUTH0_DOMAIN work
// as a .env entry, a real env var, or a -auth0-domain flag interchangeably.
func loadConfig() *koanf.Koanf {
	fs := flag.NewFlagSet("chores", flag.ExitOnError)
	fs.String("addr", ":8080", "address to listen on")
	fs.String("db", "chores.db", "path to the sqlite database file")
	fs.String("auth0-domain", "", "Auth0 tenant domain, e.g. your-tenant.eu.auth0.com — or a full http://host:port base URL for a non-Auth0 issuer such as cmd/devauth (env: AUTH0_DOMAIN)")
	fs.String("auth0-client-id", "", "Auth0 application client ID (env: AUTH0_CLIENT_ID)")
	fs.String("auth0-client-secret", "", "Auth0 application client secret (env: AUTH0_CLIENT_SECRET)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	envKeys := map[string]string{
		"AUTH0_DOMAIN":        "auth0-domain",
		"AUTH0_CLIENT_ID":     "auth0-client-id",
		"AUTH0_CLIENT_SECRET": "auth0-client-secret",
	}
	mapEnvKey := func(s string) string { return envKeys[s] }

	k := koanf.New(".")

	// .env is a local-dev convenience only, never required — a missing
	// file is fine, but a malformed one still fails startup so it doesn't
	// silently get ignored.
	if err := k.Load(file.Provider(".env"), dotenv.ParserEnv("", ".", mapEnvKey)); err != nil && !os.IsNotExist(err) {
		log.Fatalf("load .env: %v", err)
	}
	if err := k.Load(env.Provider("", ".", mapEnvKey), nil); err != nil {
		log.Fatalf("load env config: %v", err)
	}
	if err := k.Load(basicflag.Provider(fs, ".", &basicflag.Opt{KeyMap: k}), nil); err != nil {
		log.Fatalf("load flag config: %v", err)
	}
	return k
}

func main() {
	// Go's mime package doesn't know the .webmanifest extension out of the
	// box, which would otherwise get served as text/plain and trip up
	// browsers' PWA installability checks.
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Fatalf("register .webmanifest mime type: %v", err)
	}

	cfg := loadConfig()
	addr := cfg.String("addr")
	dbPath := cfg.String("db")
	auth0Domain := cfg.String("auth0-domain")
	auth0ClientID := cfg.String("auth0-client-id")
	auth0ClientSecret := cfg.String("auth0-client-secret")

	if auth0Domain == "" || auth0ClientID == "" || auth0ClientSecret == "" {
		log.Fatalf("auth configuration is required: set AUTH0_DOMAIN, AUTH0_CLIENT_ID and AUTH0_CLIENT_SECRET " +
			"(via .env, environment variables, or the -auth0-* flags) — for local testing without a real Auth0 " +
			"tenant, run `go run ./cmd/devauth` and point these at it instead")
	}

	authMgr, err := auth.NewManager(auth.Config{
		Domain:       auth0Domain,
		ClientID:     auth0ClientID,
		ClientSecret: auth0ClientSecret,
	})
	if err != nil {
		log.Fatalf("configure auth: %v", err)
	}

	log.Printf("auth: login required (domain=%s)", auth0Domain)

	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	svc := server.New(conn)
	// Occurrence history is kept for a bounded window (see retentionDays).
	// The earnings a purge removes are carried into each child's ledger in
	// the same transaction, so a balance never depends on whether — or
	// when — this has run.
	svc.StartRetention(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", authMgr.LoginHandler)
	mux.HandleFunc("/auth/callback", authMgr.CallbackHandler)
	mux.HandleFunc("/auth/logout", authMgr.LogoutHandler)
	mux.HandleFunc("/auth/me", authMgr.MeHandler)

	path, handler := choresv1connect.NewChoresServiceHandler(svc, server.JSONCodecOption())
	mux.Handle(path, svc.DashboardOrAuth(authMgr, handler))

	mux.Handle("/invite/accept", authMgr.RequirePage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptInvitationHandler(w, r, svc)
	})))

	// The kiosk dashboard is its own entry point, deliberately never behind
	// the login gate — it authorizes itself with a per-family
	// dashboard key instead (typed in or carried in ?key=, handled entirely
	// client-side; see web/app.js). It's the same app shell as "/", just
	// reached without a session.
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		data, err := web.FS.ReadFile("index.html")
		if err != nil {
			web.RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	// Registered ahead of the static file server (and outside the login
	// gate, since the welcome page needs it too) because the manifest's name
	// is localized per request — see web/manifest.go.
	mux.HandleFunc("/manifest.webmanifest", web.ManifestHandler)

	mux.Handle("/", authMgr.Gate(notFoundPage(http.FileServerFS(web.FS)), http.HandlerFunc(loginPageHandler)))

	log.Printf("Chores listening on %s (db: %s)", addr, dbPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// notFoundPage wraps the static file server so a bogus or stale URL (e.g. an
// old bookmark) gets the same styled error page as everything else, instead
// of Go's bare "404 page not found" text.
func notFoundPage(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		if _, err := fs.Stat(web.FS, name); err != nil {
			web.RenderErrorPage(w, http.StatusNotFound, "Page not found.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	data, err := web.FS.ReadFile("login.html")
	if err != nil {
		web.RenderErrorPage(w, http.StatusInternalServerError, "Internal error.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

// acceptInvitationHandler binds the now-authenticated caller (RequirePage
// has already ensured that and put their identity in the request context)
// to the invitation's parent slot, then sends them into the app.
func acceptInvitationHandler(w http.ResponseWriter, r *http.Request, svc *server.Server) {
	token := r.URL.Query().Get("token")
	if token == "" {
		web.RenderErrorPage(w, http.StatusBadRequest, "This invite link is missing its token.")
		return
	}
	if _, err := svc.AcceptInvitation(r.Context(), connect.NewRequest(&v1.AcceptInvitationRequest{Token: token})); err != nil {
		web.RenderErrorPage(w, http.StatusBadRequest, "Could not accept invitation: "+connectErrorMessage(err))
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

// connectErrorMessage strips a Connect error down to its underlying message
// (e.g. "you are already a member of this family"), dropping the leading
// "failed_precondition:"-style code prefix that means nothing to a user
// reading it in the browser.
func connectErrorMessage(err error) string {
	var connectErr *connect.Error
	if errors.As(err, &connectErr) {
		return connectErr.Message()
	}
	return err.Error()
}
