package auth

import "testing"

// Pinned channel prefixes ("slug-type#n" for credentials, "slug-provider" for
// AI providers) flow through the plain prefix machinery; these regressions pin
// the '#' behaviour down.
func TestPinnedPrefixRewriteAndEnabledModels(t *testing.T) {
	a := &Auth{Prefix: "573-kimi#2", Metadata: map[string]any{
		"owner":       "alice",
		"channel_id":  "ch-1",
		"prefix":      "573-kimi#2",
		"published":   true,
		"credentials": "ignored",
	}}
	if got := rewriteModelForAuth("573-kimi#2/kimi-k3", a); got != "kimi-k3" {
		t.Fatalf("pinned prefix not stripped: %q", got)
	}
	if got := rewriteModelForAuth("573-kimi#1/kimi-k3", a); got != "573-kimi#1/kimi-k3" {
		t.Fatalf("another credential's pinned prefix was stripped: %q", got)
	}
	if got := rewriteModelForAuth("573-daye/kimi-k3", a); got != "573-daye/kimi-k3" {
		t.Fatalf("provider pinned prefix was stripped by a credential: %q", got)
	}
	if !a.ChannelPublished() {
		t.Fatal("pinned credential not considered published")
	}

	a.Metadata["enabled_models"] = []any{"kimi-k3"}
	if !a.ChannelModelEnabled("573-kimi#2/kimi-k3") {
		t.Fatal("allowlisted pinned model rejected")
	}
	if a.ChannelModelEnabled("573-kimi#2/kimi-k4") {
		t.Fatal("unlisted pinned model accepted")
	}
}

func TestProviderPinnedPrefixWithoutIndex(t *testing.T) {
	a := &Auth{Prefix: "573-daye", Metadata: map[string]any{"owner": "alice", "channel_id": "ch-1", "prefix": "573-daye"}}
	if got := rewriteModelForAuth("573-daye/kimi-k3", a); got != "kimi-k3" {
		t.Fatalf("provider prefix not stripped: %q", got)
	}
	// A credential pinned prefix of the same channel is a different route.
	if got := rewriteModelForAuth("573-kimi#1/kimi-k3", a); got != "573-kimi#1/kimi-k3" {
		t.Fatalf("credential prefix stripped by provider auth: %q", got)
	}
}
