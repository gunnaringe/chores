// Command ukelonn runs the Ukelønn family allowance tracker: a single
// binary serving both the embedded web UI and the Connect API, backed by
// a local SQLite database.
package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/gunnaringe/ukelonn/gen/ukelonn/v1/ukelonnv1connect"
	"github.com/gunnaringe/ukelonn/internal/db"
	"github.com/gunnaringe/ukelonn/internal/server"
	"github.com/gunnaringe/ukelonn/web"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dbPath := flag.String("db", "ukelonn.db", "path to the sqlite database file")
	flag.Parse()

	conn, err := db.Open(*dbPath)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer conn.Close()

	svc := server.New(conn)

	mux := http.NewServeMux()
	path, handler := ukelonnv1connect.NewUkelonnServiceHandler(svc)
	mux.Handle(path, handler)
	mux.Handle("/", http.FileServerFS(web.FS))

	log.Printf("ukelønn listening on %s (db: %s)", *addr, *dbPath)
	if err := http.ListenAndServe(*addr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
