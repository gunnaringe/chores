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

	v1 "github.com/gunnaringe/chores/gen/chores/v1"
	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
	"github.com/gunnaringe/chores/internal/auth"
	"github.com/gunnaringe/chores/internal/db"
	"github.com/gunnaringe/chores/internal/server"
	"github.com/gunnaringe/chores/web"
)

func main() {
	// Go's mime package doesn't know the .webmanifest extension out of the
	// box, which would otherwise get served as text/plain and trip up
	// browsers' PWA installability checks.
	if err := mime.AddExtensionType(".webmanifest", "application/manifest+json"); err != nil {
		log.Fatalf("register .webmanifest mime type: %v", err)
	}

	addr := flag.String("addr", ":8080", "address to listen on")
	dbPath := flag.String("db", "chores.db", "path to the sqlite database file")
	authModeFlag := flag.String("auth", "auto",
		`authentication mode: "auto" (auth0 if AUTH0_DOMAIN/AUTH0_CLIENT_ID/AUTH0_CLIENT_SECRET are set, otherwise disabled), "auth0", or "disabled"`)
	auth0Domain := flag.String("auth0-domain", os.Getenv("AUTH0_DOMAIN"), "Auth0 tenant domain, e.g. your-tenant.eu.auth0.com")
	auth0ClientID := flag.String("auth0-client-id", os.Getenv("AUTH0_CLIENT_ID"), "Auth0 application client ID")
	auth0ClientSecret := flag.String("auth0-client-secret", os.Getenv("AUTH0_CLIENT_SECRET"), "Auth0 application client secret")
	auth0CallbackURL := flag.String("auth0-callback-url", os.Getenv("AUTH0_CALLBACK_URL"), "full callback URL registered with Auth0, e.g. http://localhost:8080/auth/callback (defaults to http://localhost<addr>/auth/callback)")
	flag.Parse()

	mode := auth.ModeDisabled
	switch *authModeFlag {
	case "auto":
		if *auth0Domain != "" && *auth0ClientID != "" && *auth0ClientSecret != "" {
			mode = auth.ModeAuth0
		}
	case "auth0":
		mode = auth.ModeAuth0
	case "disabled":
		mode = auth.ModeDisabled
	default:
		log.Fatalf("invalid -auth value %q (want auto, auth0, or disabled)", *authModeFlag)
	}

	callbackURL := *auth0CallbackURL
	if mode == auth.ModeAuth0 && callbackURL == "" {
		callbackURL = "http://localhost" + *addr + "/auth/callback"
	}

	authMgr, err := auth.NewManager(auth.Config{
		Mode:         mode,
		Domain:       *auth0Domain,
		ClientID:     *auth0ClientID,
		ClientSecret: *auth0ClientSecret,
		CallbackURL:  callbackURL,
	})
	if err != nil {
		log.Fatalf("configure auth: %v", err)
	}

	if mode == auth.ModeDisabled {
		log.Printf("auth: disabled (local testing mode) — anyone can access the app without logging in")
	} else {
		log.Printf("auth: auth0 login required (domain=%s, callback=%s)", *auth0Domain, callbackURL)
	}

	conn, err := db.Open(*dbPath)
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
	mux.Handle(path, authMgr.RequireAuth(handler))

	mux.Handle("/invite/accept", authMgr.RequirePage(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptInvitationHandler(w, r, svc)
	})))

	mux.Handle("/", authMgr.Gate(http.FileServerFS(web.FS), http.HandlerFunc(loginPageHandler)))

	log.Printf("Chores listening on %s (db: %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, mux); err != nil {
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
