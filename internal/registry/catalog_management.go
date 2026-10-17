package registry

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// ModelCatalogStatus describes refresh policy without disclosing configured URLs.
type ModelCatalogStatus struct {
	Kind                   string `json:"kind"`
	AutoRefresh            bool   `json:"auto_refresh"`
	RefreshIntervalSeconds int64  `json:"refresh_interval_seconds"`
	SupportsRefresh        bool   `json:"supports_refresh"`
	UpstreamDiscovery      bool   `json:"upstream_discovery"`
}

func catalogUpdaterForProvider(provider string) *catalogUpdater {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "zcode", "antigravity":
		return nil
	case "devin":
		return devinCatalogUpdater
	default:
		return generalCatalogUpdater
	}
}

func ModelCatalogStatusForProvider(provider string) ModelCatalogStatus {
	if strings.EqualFold(strings.TrimSpace(provider), "antigravity") {
		return ModelCatalogStatus{Kind: "account_upstream", AutoRefresh: true, SupportsRefresh: true, UpstreamDiscovery: true, RefreshIntervalSeconds: int64(ModelsRefreshInterval / time.Second)}
	}
	u := catalogUpdaterForProvider(provider)
	status := ModelCatalogStatus{Kind: "builtin"}
	if u == nil {
		return status
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.ctx == nil {
		return status
	}
	switch u.source {
	case embeddedCatalogSource:
		return status
	case "disabled":
		status.Kind = "disabled"
		return status
	default:
		status.Kind = "remote_catalog"
		if filepath.IsAbs(u.source) {
			status.Kind = "file_catalog"
		}
		status.AutoRefresh = u.ctx.Err() == nil
		status.SupportsRefresh = status.AutoRefresh
		status.RefreshIntervalSeconds = int64(ModelsRefreshInterval / time.Second)
		return status
	}
}

// RefreshModelCatalogForProvider honors the active source, including local-only
// mode. A failed manual refresh is reported instead of looking like a fresh hit.
func RefreshModelCatalogForProvider(ctx context.Context, provider string) error {
	u := catalogUpdaterForProvider(provider)
	if u == nil {
		return fmt.Errorf("provider uses a built-in model catalog")
	}
	u.mu.Lock()
	source, generation, active := u.source, u.generation, u.ctx
	u.mu.Unlock()
	if active == nil || active.Err() != nil || source == "disabled" || source == embeddedCatalogSource {
		return fmt.Errorf("model catalog refresh is disabled")
	}
	return u.refreshWithError(ctx, source, generation)
}
