package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gunnaringe/chores/gen/chores/v1/choresv1connect"
)

// A paused task's "active: false" must actually appear on the wire, not be
// silently dropped — the frontend distinguishes "paused" from "active" with
// a strict `=== false` check, which only works if the zero value is present.
// protojson's default MarshalOptions omits unpopulated (zero-value) fields,
// making a paused task indistinguishable from one that never sent "active"
// at all; JSONCodecOption's EmitDefaultValues is what fixes that.
func TestJSONCodec_EmitsFalseActiveField(t *testing.T) {
	svc := newTestServer(t)
	path, handler := choresv1connect.NewChoresServiceHandler(svc, JSONCodecOption())
	mux := http.NewServeMux()
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	post := func(method string, body map[string]any) map[string]any {
		t.Helper()
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		resp, err := http.Post(srv.URL+path+method, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("POST %s: %v", method, err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatalf("decode %s response: %v", method, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("POST %s: status %d, body %v", method, resp.StatusCode, out)
		}
		return out
	}

	family := post("CreateFamily", map[string]any{"name": "Wire Family"})["family"].(map[string]any)
	familyID := family["id"].(string)
	child := post("CreateUser", map[string]any{"familyId": familyID, "name": "Kid", "role": "USER_ROLE_CHILD"})["user"].(map[string]any)
	childID := child["id"].(string)
	task := post("CreateTask", map[string]any{
		"familyId": familyID, "title": "Sweep", "priceCents": 100, "schedule": "0 0 * * *", "repeatMode": "REPEAT_MODE_CRON", "childIds": []string{childID},
	})["task"].(map[string]any)
	taskID := task["id"].(string)

	post("UpdateTask", map[string]any{
		"taskId": taskID, "title": "Sweep", "priceCents": 100, "schedule": "0 0 * * *", "repeatMode": "REPEAT_MODE_CRON", "childIds": []string{childID}, "active": false,
	})

	// Read the raw body directly instead of decoding into a map, since a Go
	// map would treat "active" being absent and "active": false identically.
	raw, err := json.Marshal(map[string]any{"familyId": familyID})
	if err != nil {
		t.Fatalf("marshal ListTasks request: %v", err)
	}
	resp, err := http.Post(srv.URL+path+"ListTasks", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST ListTasks: %v", err)
	}
	defer resp.Body.Close()
	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read ListTasks body: %v", err)
	}

	if !strings.Contains(body.String(), `"active":false`) {
		t.Fatalf(`expected the paused task's response to contain literal "active":false, got: %s`, body.String())
	}
}
