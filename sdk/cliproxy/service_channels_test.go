package cliproxy

import (
	"testing"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func TestMultiUserModelsRequirePublishedOwnedChannel(t *testing.T) {
	models := []*ModelInfo{{ID: "gpt-5"}, {ID: "alice-gpt/gpt-5"}, {ID: "alice-gpt/gpt-4"}, {ID: "bob-gpt/gpt-5"}}
	a := &coreauth.Auth{Prefix: "alice-gpt", Metadata: map[string]any{"owner": "alice", "channel_id": "channel", "published": true}}
	got := filterChannelModels(a, models, true)
	if len(got) != 2 || got[0].ID != "alice-gpt/gpt-5" {
		t.Fatalf("bare or other channel models leaked: %#v", got)
	}
	a.Metadata["enabled_models"] = []any{"gpt-5"}
	got = filterChannelModels(a, models, true)
	if len(got) != 1 || got[0].ID != "alice-gpt/gpt-5" {
		t.Fatal("model allowlist not enforced")
	}
	a.Metadata["published"] = false
	if len(filterChannelModels(a, models, true)) != 0 {
		t.Fatal("unpublished channel listed")
	}
	if len(filterChannelModels(a, models, false)) != 4 {
		t.Fatal("single-user behavior changed")
	}
	delete(a.Metadata, "owner")
	a.Metadata["published"] = true
	if len(filterChannelModels(a, models, true)) != 0 {
		t.Fatal("unclaimed credentials listed")
	}
}
