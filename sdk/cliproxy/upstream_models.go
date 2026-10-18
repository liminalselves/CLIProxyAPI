package cliproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	zcodeauth "github.com/router-for-me/CLIProxyAPI/v8/internal/auth/zcode"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/proxyutil"
	log "github.com/sirupsen/logrus"
)

// Providers whose catalog lives behind the credential's own upstream expose an
// OpenAI-style GET {baseURL}/models listing. Results are cached per credential
// so the management panel does not hit the upstream on every open.
var (
	upstreamModelsCacheMu sync.RWMutex
	upstreamModelsCache   = make(map[string]upstreamModelsCacheEntry)
)

type upstreamModelsCacheEntry struct {
	models    []*ModelInfo
	expiresAt time.Time
}

func cachedUpstreamModels(a *coreauth.Auth) []*ModelInfo {
	if a == nil {
		return nil
	}
	upstreamModelsCacheMu.RLock()
	entry, ok := upstreamModelsCache[a.ID]
	upstreamModelsCacheMu.RUnlock()
	if !ok || time.Now().After(entry.expiresAt) {
		return nil
	}
	return entry.models
}

func storeUpstreamModels(a *coreauth.Auth, models []*ModelInfo) {
	if a == nil || a.ID == "" {
		return
	}
	upstreamModelsCacheMu.Lock()
	upstreamModelsCache[a.ID] = upstreamModelsCacheEntry{
		models:    models,
		expiresAt: time.Now().Add(registry.ModelsRefreshInterval),
	}
	upstreamModelsCacheMu.Unlock()
}

// upstreamModelsEndpoint resolves the listing endpoint and credential material
// for providers with an account-scoped upstream catalog.
func upstreamModelsEndpoint(a *coreauth.Auth, provider string) (baseURL, apiKey string) {
	if a == nil {
		return "", ""
	}
	metadataString := func(key string) string {
		value, _ := a.Metadata[key].(string)
		return strings.TrimSpace(value)
	}
	switch provider {
	case "zcode":
		baseURL = metadataString("base_url")
		if baseURL == "" {
			baseURL = zcodeauth.DefaultAPIBaseURL
		}
		apiKey = metadataString("access_token")
	case "openai-compatible":
		baseURL = metadataString("base_url")
		apiKey = metadataString("api_key")
		if apiKey == "" && a.Attributes != nil {
			apiKey = strings.TrimSpace(a.Attributes["api_key"])
		}
	}
	return baseURL, apiKey
}

// fetchUpstreamModels reads an OpenAI-style model listing. The credential's own
// proxy applies; a missing key or non-2xx response is a refresh failure.
func fetchUpstreamModels(ctx context.Context, baseURL, apiKey, proxyURL string) ([]*ModelInfo, error) {
	if strings.TrimSpace(baseURL) == "" || strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("upstream endpoint or credential is missing")
	}
	client := &http.Client{}
	transport, _, errProxy := proxyutil.BuildHTTPTransport(proxyURL)
	if errProxy != nil {
		return nil, errProxy
	}
	if transport != nil {
		client.Transport = transport
		defer transport.CloseIdleConnections()
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/models"
	req, errReq := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if errReq != nil {
		return nil, errReq
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	resp, errDo := client.Do(req)
	if errDo != nil {
		return nil, errDo
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Debug("upstream model fetch: response close failed")
		}
	}()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream model listing returned status %d", resp.StatusCode)
	}
	const maxCatalogBytes = 8 << 20
	data, errRead := io.ReadAll(io.LimitReader(resp.Body, maxCatalogBytes+1))
	if errRead != nil || len(data) > maxCatalogBytes {
		return nil, fmt.Errorf("upstream model listing could not be read")
	}
	var payload struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if errUnmarshal := json.Unmarshal(data, &payload); errUnmarshal != nil {
		return nil, fmt.Errorf("upstream model listing is not an OpenAI-style response")
	}
	models := make([]*ModelInfo, 0, len(payload.Data))
	seen := make(map[string]bool, len(payload.Data))
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		ownedBy := strings.TrimSpace(item.OwnedBy)
		models = append(models, &ModelInfo{ID: id, Object: "model", OwnedBy: ownedBy, Type: ownedBy})
	}
	return models, nil
}
