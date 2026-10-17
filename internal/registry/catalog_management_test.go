package registry

import (
	"context"
	"errors"
	"testing"
)

func TestManualCatalogRefreshHonorsSourceAndFailure(t *testing.T) {
	previous := generalCatalogUpdater
	t.Cleanup(func() { generalCatalogUpdater = previous })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	published := 0
	u := &catalogUpdater{ctx: ctx, source: "https://catalog.example.com/models.json", generation: 1,
		fetch: func(_ context.Context, source string) ([]byte, error) {
			if source != "https://catalog.example.com/models.json" {
				t.Fatal("manual refresh ignored configured source")
			}
			return []byte("catalog"), nil
		},
		publish: func([]byte) ([]string, error) { published++; return nil, nil },
	}
	generalCatalogUpdater = u
	status := ModelCatalogStatusForProvider("codex")
	if status.Kind != "remote_catalog" || status.UpstreamDiscovery || !status.SupportsRefresh || !status.AutoRefresh || status.RefreshIntervalSeconds != 10800 {
		t.Fatalf("source policy: %#v", status)
	}
	if err := RefreshModelCatalogForProvider(ctx, "codex"); err != nil || published != 1 {
		t.Fatalf("manual refresh: %d %v", published, err)
	}
	u.fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("unavailable") }
	if err := RefreshModelCatalogForProvider(ctx, "codex"); err == nil || published != 1 {
		t.Fatal("failed manual refresh published data or reported success")
	}
	u.source = embeddedCatalogSource
	if status := ModelCatalogStatusForProvider("codex"); status.AutoRefresh || status.SupportsRefresh || status.Kind != "builtin" {
		t.Fatalf("embedded policy: %#v", status)
	}
	if err := RefreshModelCatalogForProvider(ctx, "codex"); err == nil {
		t.Fatal("local-only mode attempted a remote refresh")
	}
	u.source = "disabled"
	if status := ModelCatalogStatusForProvider("codex"); status.Kind != "disabled" || status.SupportsRefresh {
		t.Fatalf("disabled policy: %#v", status)
	}
	if status := ModelCatalogStatusForProvider("zcode"); status.Kind != "builtin" || status.SupportsRefresh {
		t.Fatalf("ZCode policy: %#v", status)
	}
}

func TestManualCatalogRefreshRejectsSourceChanges(t *testing.T) {
	previous := generalCatalogUpdater
	t.Cleanup(func() { generalCatalogUpdater = previous })
	published := false
	u := &catalogUpdater{ctx: context.Background(), source: "https://catalog.example.com/models.json", generation: 1}
	u.fetch = func(context.Context, string) ([]byte, error) {
		u.mu.Lock()
		u.generation++
		u.mu.Unlock()
		return []byte("stale"), nil
	}
	u.publish = func([]byte) ([]string, error) { published = true; return nil, nil }
	generalCatalogUpdater = u
	if err := RefreshModelCatalogForProvider(context.Background(), "codex"); err == nil || published {
		t.Fatal("source change published a stale catalog")
	}
}
