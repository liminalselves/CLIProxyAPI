package auth

import (
	"context"
	"testing"
)

func channelCredential(prefix, channel, credType string) *Auth {
	return &Auth{Prefix: prefix, Metadata: map[string]any{
		"channel_prefix":  channel,
		"credential_type": credType,
	}}
}

// The blocklist scopes a single pinned route without touching same-named
// models on other channels or credentials.
func TestBlocklistPinnedRouteIsExact(t *testing.T) {
	kimiA := channelCredential("123-kimi#1", "123", "kimi")
	kimiB := channelCredential("123-kimi#2", "123", "kimi")
	other := channelCredential("456-kimi#1", "456", "kimi")
	blocked := WithBlockedModels(context.Background(), []string{"123-kimi#1/gpt-6"})
	list := blockedModelsFromContext(blocked)

	if !list.blocksAuth(kimiA, "gpt-6") {
		t.Fatal("pinned route must block its own credential for the bare model")
	}
	if list.blocksAuth(kimiB, "gpt-6") {
		t.Fatal("pinned route must not block a sibling credential of the same channel")
	}
	if list.blocksAuth(other, "gpt-6") {
		t.Fatal("pinned route must not block the same model on another channel")
	}
	if !list.blocksAuth(kimiA, "123-kimi#1/gpt-6") {
		t.Fatal("pinned route must block a pinned request for itself")
	}
	if !ModelBlocked(blocked, "123-kimi#1/gpt-6") {
		t.Fatal("ModelBlocked must match the pinned route string")
	}
	if ModelBlocked(blocked, "123-kimi#2/gpt-6") || ModelBlocked(blocked, "gpt-6") {
		t.Fatal("ModelBlocked must not widen a pinned route block")
	}
}

func TestBlocklistBareModelBlocksEveryRoute(t *testing.T) {
	kimiA := channelCredential("123-kimi#1", "123", "kimi")
	other := channelCredential("456-kimi#1", "456", "kimi")
	blocked := WithBlockedModels(context.Background(), []string{"gpt-6"})
	list := blockedModelsFromContext(blocked)

	for i, auth := range []*Auth{kimiA, other} {
		if !list.blocksAuth(auth, "gpt-6") || !list.blocksAuth(auth, auth.Prefix+"/gpt-6") {
			t.Fatalf("bare model must block auth %d on every route", i)
		}
		if list.blocksAuth(auth, "gpt-7") {
			t.Fatalf("bare model must not block other models on auth %d", i)
		}
	}
	if !ModelBlocked(blocked, "gpt-6") || !AuthModelBlocked(blocked, other, "456-kimi#1/gpt-6") {
		t.Fatal("bare exclusions must match literal names and credential-resolved routes")
	}
}

func TestBlocklistChannelAndCredentialScopes(t *testing.T) {
	kimiA := channelCredential("123-kimi#1", "123", "kimi")
	kimiB := channelCredential("123-kimi#2", "123", "kimi")
	claude := channelCredential("123-claude#1", "123", "claude")
	provider := &Auth{Prefix: "123-ext", Metadata: map[string]any{"channel_prefix": "123", "provider_name": "ext"}}
	other := channelCredential("456-kimi#1", "456", "kimi")

	// Whole channel.
	list := blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"123"}))
	for i, auth := range []*Auth{kimiA, kimiB, claude, provider} {
		if !list.blocksAuth(auth, "gpt-6") {
			t.Fatalf("channel block must cover auth %d (%s)", i, auth.Prefix)
		}
	}
	if list.blocksAuth(other, "gpt-6") {
		t.Fatal("channel block must not reach another channel")
	}

	// Credential type within one channel.
	list = blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"123-kimi"}))
	if !list.blocksAuth(kimiA, "gpt-6") || !list.blocksAuth(kimiB, "gpt-6") {
		t.Fatal("credential-type block must cover every credential of that type in the channel")
	}
	if list.blocksAuth(claude, "gpt-6") {
		t.Fatal("credential-type block must not cover other types in the channel")
	}
	if list.blocksAuth(provider, "gpt-6") {
		t.Fatal("credential-type block must not cover a provider route with a different prefix")
	}
	if list.blocksAuth(other, "gpt-6") {
		t.Fatal("credential-type block must not reach another channel")
	}

	// Single credential.
	list = blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"123-kimi#1"}))
	if !list.blocksAuth(kimiA, "gpt-6") {
		t.Fatal("credential block must cover the credential itself")
	}
	if list.blocksAuth(kimiB, "gpt-6") || list.blocksAuth(claude, "gpt-6") {
		t.Fatal("credential block must not cover sibling credentials")
	}

	// Provider route prefix.
	list = blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"123-ext"}))
	if !list.blocksAuth(provider, "gpt-6") {
		t.Fatal("provider prefix block must cover the provider route")
	}
	if list.blocksAuth(kimiA, "gpt-6") {
		t.Fatal("provider prefix block must not cover credentials")
	}
}

func TestBlocklistScopeAppliesToPinnedRequests(t *testing.T) {
	kimiA := channelCredential("123-kimi#1", "123", "kimi")
	other := channelCredential("456-kimi#1", "456", "kimi")
	list := blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"123"}))

	if !list.blocksAuth(kimiA, "123-kimi#1/gpt-6") {
		t.Fatal("channel block must block a pinned request served by its own credential")
	}
	if list.blocksAuth(other, "123-kimi#1/gpt-6") {
		t.Fatal("a credential pinned elsewhere is not blocked for a route it cannot serve")
	}
}

func TestBlocklistEmptyAndUnscopedAuth(t *testing.T) {
	if blockedModelsFromContext(context.Background()).blocksAuth(&Auth{Prefix: "x"}, "m") {
		t.Fatal("empty blocklist must not block")
	}
	plain := &Auth{Prefix: "solo"}
	list := blockedModelsFromContext(WithBlockedModels(context.Background(), []string{"solo", "solo/gpt-6", "gpt-6"}))
	if !list.blocksAuth(plain, "gpt-6") {
		t.Fatal("a credential without channel metadata is still matched by its own prefix")
	}
}
