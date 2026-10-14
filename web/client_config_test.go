package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestClientConfigHandler(t *testing.T) {
	rec := httptest.NewRecorder()
	ClientConfigHandler("api.example.test").ServeHTTP(rec, httptest.NewRequest("GET", "/client-config.json", nil))
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["grpcHost"] != "api.example.test" {
		t.Fatalf("grpcHost = %q", got["grpcHost"])
	}
}
