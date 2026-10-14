package web

import (
	"encoding/json"
	"net/http"
)

// ClientConfigHandler serves the few deployment settings the frontend needs
// to know about, as JSON. Public and identical for everyone, so it's
// outside the login gate. grpcHost is the host[:port] gRPC clients should
// use when it isn't the page's own (see the grpc-host flag), or "".
func ClientConfigHandler(grpcHost string) http.Handler {
	body, _ := json.Marshal(map[string]string{"grpcHost": grpcHost})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Changes only with a restart; no reason to cache across one.
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(body)
	})
}
