package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteUnauthenticated(t *testing.T) {
	// The browser's Connect JSON call: unchanged 401 with a JSON body.
	req := httptest.NewRequest("POST", "/chores.v1.ChoresService/ListFamilies", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	WriteUnauthenticated(rec, req, "login required")
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code":"unauthenticated"`) {
		t.Fatalf("connect: code %d, body %s", rec.Code, rec.Body.String())
	}

	// A gRPC client: HTTP 200 with grpc-status 16 (Unauthenticated), which is
	// what grpcurl needs to say "Unauthenticated" instead of a protocol error.
	req = httptest.NewRequest("POST", "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.ProtoMajor = 2
	rec = httptest.NewRecorder()
	WriteUnauthenticated(rec, req, "login required")
	status := rec.Header().Get("Grpc-Status")
	if status == "" {
		status = rec.Result().Trailer.Get("Grpc-Status")
	}
	if status != "16" {
		t.Fatalf("grpc: grpc-status %q, headers %v", status, rec.Header())
	}
}
