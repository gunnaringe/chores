package web

import "testing"

func TestETag(t *testing.T) {
	a, b := ETag("app.js"), ETag("app.css")
	if a == "" || b == "" || a == b {
		t.Fatalf("want distinct non-empty tags, got %q and %q", a, b)
	}
	if again := ETag("app.js"); again != a {
		t.Fatalf("not stable: %q then %q", a, again)
	}
	if got := ETag("nope.js"); got != "" {
		t.Fatalf("missing file: got %q, want empty", got)
	}
}
