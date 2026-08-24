// Command chores runs Chores, a family allowance and chore tracker: a
// single binary serving both the embedded web UI and the Connect API,
// backed by a local SQLite database.
package main

import (
	"flag"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"

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
	fs.String("auth", "auto",
		`authentication mode: "auto" (auth0 if AUTH0_DOMAIN/AUTH0_CLIENT_ID/AUTH0_CLIENT_SECRET are set, otherwise disabled), "auth0", or "disabled"`)
	fs.String("auth0-domain", "", "Auth0 tenant domain, e.g. your-tenant.eu.auth0.com (env: AUTH0_DOMAIN)")
	fs.String("auth0-client-id", "", "Auth0 application client ID (env: AUTH0_CLIENT_ID)")
	fs.String("auth0-client-secret", "", "Auth0 application client secret (env: AUTH0_CLIENT_SECRET)")
	fs.String("auth0-callback-url", "", "full callback URL registered with Auth0, e.g. http://localhost:8080/auth/callback (defaults to http://localhost<addr>/auth/callback) (env: AUTH0_CALLBACK_URL)")
	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatalf("parse flags: %v", err)
	}

	envKeys := map[string]string{
		"AUTH0_DOMAIN":        "auth0-domain",
		"AUTH0_CLIENT_ID":     "auth0-client-id",
		"AUTH0_CLIENT_SECRET": "auth0-client-secret",
		"AUTH0_CALLBACK_URL":  "auth0-callback-url",
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

	mode := auth.ModeDisabled
	switch cfg.String("auth") {
	case "auto":
		if auth0Domain != "" && auth0ClientID != "" && auth0ClientSecret != "" {
			mode = auth.ModeAuth0
		}
	case "auth0":
		mode = auth.ModeAuth0
	case "disabled":
		mode = auth.ModeDisabled
	default:
		log.Fatalf("invalid -auth value %q (want auto, auth0, or disabled)", cfg.String("auth"))
	}

	callbackURL := cfg.String("auth0-callback-url")
	if mode == auth.ModeAuth0 && callbackURL == "" {
		callbackURL = "http://localhost" + addr + "/auth/callback"
	}

	authMgr, err := auth.NewManager(auth.Config{
		Mode:         mode,
		Domain:       auth0Domain,
		ClientID:     auth0ClientID,
		ClientSecret: auth0ClientSecret,
		CallbackURL:  callbackURL,
	})
	if err != nil {
		log.Fatalf("configure auth: %v", err)
	}

	if mode == auth.ModeDisabled {
		log.Printf("auth: disabled (local testing mode) — anyone can access the app without logging in")
	} else {
		log.Printf("auth: auth0 login required (domain=%s, callback=%s)", auth0Domain, callbackURL)
	}

	conn, err := db.Open(dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	svc := server.New(conn)

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
	// the Auth0 login gate — it authorizes itself with a per-family
	// dashboard key instead (typed in or carried in ?key=, handled entirely
	// client-side; see web/app.js). It's the same app shell as "/", just
	// reached without a session.
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		data, err := web.FS.ReadFile("index.html")
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	mux.Handle("/", authMgr.Gate(http.FileServerFS(web.FS), http.HandlerFunc(loginPageHandler)))

	log.Printf("Chores listening on %s (db: %s)", addr, dbPath)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func loginPageHandler(w http.ResponseWriter, r *http.Request) {
	data, err := web.FS.ReadFile("login.html")
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.Error(w, "missing invitation token", http.StatusBadRequest)
		return
	}
	if _, err := svc.AcceptInvitation(r.Context(), connect.NewRequest(&v1.AcceptInvitationRequest{Token: token})); err != nil {
		http.Error(w, fmt.Sprintf("could not accept invitation: %v", err), http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}
