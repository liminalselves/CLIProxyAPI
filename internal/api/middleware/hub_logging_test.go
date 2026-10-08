package middleware

import "testing"

func TestHubAccountTrafficIsExcludedFromRequestLogs(t *testing.T) {
	for _, path := range []string{"/v8/hub/login", "/v8/hub/register", "/v8/hub/credentials", "/v8/hub/keys", "/v8/hub/oauth/callback"} {
		if shouldLogRequest(path) {
			t.Fatalf("credential or account payload logged: %s", path)
		}
	}
	if !shouldLogRequest("/v1/chat/completions") {
		t.Fatal("proxy logging disabled accidentally")
	}
}
