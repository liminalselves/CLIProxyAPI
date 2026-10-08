package management

import "testing"

func TestHubQuotaRouteAllowlist(t *testing.T) {
	allowed := []struct {
		provider string
		host     string
		path     string
	}{
		{"kimi", "api.kimi.com", "/coding/v1/usages"},
		{"kimi", "api.kimi.ai", "/coding/v1/usages"},
		{"claude", "api.anthropic.com", "/api/oauth/usage"},
		{"codex", "chatgpt.com", "/backend-api/wham/usage"},
		{"codex", "chatgpt.com", "/backend-api/subscriptions"},
		{"antigravity", "cloudcode-pa.googleapis.com", "/v1internal:retrieveUserQuotaSummary"},
		{"xai", "cli-chat-proxy.grok.com", "/v1/user"},
		{"meta", "api.meta.ai", "/muse-code/key"},
		{"devin", "server.codeium.com", "/exa.seat_management_pb.SeatManagementService/GetUserStatus"},
	}
	for _, tc := range allowed {
		if !hubQuotaRouteAllowed(tc.provider, tc.host, tc.path) {
			t.Errorf("allowed quota route rejected: %s %s%s", tc.provider, tc.host, tc.path)
		}
	}
	denied := []struct {
		provider string
		host     string
		path     string
	}{
		// Token exfiltration targets.
		{"kimi", "evil.example.com", "/coding/v1/usages"},
		{"claude", "api.anthropic.com.evil.example.com", "/api/oauth/usage"},
		// Same host but non-quota paths stay blocked.
		{"kimi", "api.kimi.com", "/v1/chat/completions"},
		{"codex", "chatgpt.com", "/backend-api/conversation"},
		// Providers without quota adapters are not proxied at all.
		{"gemini", "generativelanguage.googleapis.com", "/v1beta/models"},
		{"openai-compatible", "api.example.com", "/v1/models"},
	}
	for _, tc := range denied {
		if hubQuotaRouteAllowed(tc.provider, tc.host, tc.path) {
			t.Errorf("non-quota route allowed: %s %s%s", tc.provider, tc.host, tc.path)
		}
	}
}
