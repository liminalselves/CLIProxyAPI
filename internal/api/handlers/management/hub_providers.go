package management

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// Hub AI providers are channel-mounted OpenAI-compatible upstreams. A provider
// is stored as an auth file with type "openai-compatible" carrying base_url,
// api_key and the advertised model list in its metadata, so it flows through the
// same credential pipeline (prefix stamping, permissions, usage attribution).

type hubProviderModelInput struct {
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

type hubProviderBody struct {
	Label    *string                  `json:"label"`
	BaseURL  *string                  `json:"base_url"`
	APIKey   *string                  `json:"api_key"`
	ProxyURL *string                  `json:"proxy_url"`
	Models   *[]hubProviderModelInput `json:"models"`
	Disabled *bool                    `json:"disabled"`
}

func normalizeHubProviderBaseURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("%w: base_url is required", multiuser.ErrInvalid)
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", fmt.Errorf("%w: base_url must be an http(s) URL", multiuser.ErrInvalid)
	}
	return strings.TrimRight(value, "/"), nil
}

func buildHubProviderModels(inputs []hubProviderModelInput) ([]config.OpenAICompatibilityModel, error) {
	models := make([]config.OpenAICompatibilityModel, 0, len(inputs))
	seen := map[string]bool{}
	for _, input := range inputs {
		name := strings.TrimSpace(input.Name)
		alias := strings.TrimSpace(input.Alias)
		if name == "" {
			return nil, fmt.Errorf("%w: model name is required", multiuser.ErrInvalid)
		}
		if strings.ContainsAny(name+alias, "/#") {
			return nil, fmt.Errorf("%w: model names must not contain '/' or '#'", multiuser.ErrInvalid)
		}
		key := alias
		if key == "" {
			key = name
		}
		if seen[key] {
			return nil, fmt.Errorf("%w: duplicate model %s", multiuser.ErrInvalid, key)
		}
		seen[key] = true
		if alias == "" {
			alias = name
		}
		models = append(models, config.OpenAICompatibilityModel{Name: name, Alias: alias})
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("%w: at least one model is required", multiuser.ErrInvalid)
	}
	return models, nil
}

// HubDiscoverProviderModels fetches the upstream /models listing so the console
// can pre-fill the provider's model list. The 15s timeout is part of credential
// acquisition, not an upstream request timeout.
func (h *Handler) HubDiscoverProviderModels(c *gin.Context) {
	var body struct {
		BaseURL  string `json:"base_url"`
		APIKey   string `json:"api_key"`
		ProxyURL string `json:"proxy_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		hubError(c, multiuser.ErrInvalid)
		return
	}
	baseURL, err := normalizeHubProviderBaseURL(body.BaseURL)
	if err != nil {
		hubError(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		hubError(c, err)
		return
	}
	if key := strings.TrimSpace(body.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")
	// The optional proxy lets members discover upstreams only reachable through
	// one; the 15s context bounds the whole exchange either way.
	client := http.DefaultClient
	if proxy := strings.TrimSpace(body.ProxyURL); proxy != "" {
		proxyURL, errProxy := url.Parse(proxy)
		if errProxy != nil || proxyURL.Scheme == "" || proxyURL.Host == "" {
			hubError(c, fmt.Errorf("%w: proxy_url is not a valid URL", multiuser.ErrInvalid))
			return
		}
		client = &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)}}
	}
	resp, err := client.Do(req)
	if err != nil {
		hubError(c, fmt.Errorf("%w: upstream models request failed: %v", multiuser.ErrInvalid, err))
		return
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Error("multi-user: close provider discovery response failed")
		}
	}()
	if resp.StatusCode != http.StatusOK {
		hubError(c, fmt.Errorf("%w: upstream returned %s", multiuser.ErrInvalid, resp.Status))
		return
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, resp.Body, 4<<20))
	if errDecode := decoder.Decode(&payload); errDecode != nil {
		hubError(c, fmt.Errorf("%w: upstream models response is not valid JSON", multiuser.ErrInvalid))
		return
	}
	models := make([]string, 0, len(payload.Data))
	for _, entry := range payload.Data {
		if id := strings.TrimSpace(entry.ID); id != "" {
			models = append(models, id)
		}
	}
	c.JSON(200, gin.H{"models": models})
}

// HubCreateProvider mounts an AI provider onto a channel. The pinned routing
// prefix is "<channel-slug>-<provider-name>".
func (h *Handler) HubCreateProvider(c *gin.Context) {
	ch, ok := h.hubChannel(c, c.Query("channel_id"))
	if !ok {
		return
	}
	c.Set("hubAction", "provider.create")
	c.Set("hubTarget", ch.ID)
	if h.authManager == nil {
		c.JSON(503, gin.H{"error": "credential manager unavailable"})
		return
	}
	var body struct {
		Name     string                  `json:"name"`
		BaseURL  string                  `json:"base_url"`
		APIKey   string                  `json:"api_key"`
		ProxyURL string                  `json:"proxy_url"`
		Models   []hubProviderModelInput `json:"models"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		hubError(c, multiuser.ErrInvalid)
		return
	}
	name := strings.ToLower(strings.TrimSpace(body.Name))
	if !multiuser.IsValidSlug(name) {
		hubError(c, fmt.Errorf("%w: provider name must be 1 to 40 lowercase letters, digits or hyphens", multiuser.ErrInvalid))
		return
	}
	baseURL, err := normalizeHubProviderBaseURL(body.BaseURL)
	if err != nil {
		hubError(c, err)
		return
	}
	apiKey := strings.TrimSpace(body.APIKey)
	if apiKey == "" {
		hubError(c, fmt.Errorf("%w: api_key is required", multiuser.ErrInvalid))
		return
	}
	models, err := buildHubProviderModels(body.Models)
	if err != nil {
		hubError(c, err)
		return
	}
	pinned := ch.Prefix + "-" + name
	// The pinned prefix becomes the model ID root, so it must be unique across
	// both channel slugs and every existing credential prefix.
	for _, other := range h.hubStore.Channels() {
		if other.Slug == pinned || other.Prefix == pinned {
			hubError(c, fmt.Errorf("%w: provider name collides with channel %s", multiuser.ErrConflict, pinned))
			return
		}
	}
	for _, existing := range h.authManager.List() {
		if strings.EqualFold(strings.TrimSpace(existing.Prefix), pinned) {
			hubError(c, fmt.Errorf("%w: provider name %s is already in use", multiuser.ErrConflict, pinned))
			return
		}
	}
	actor := multiuser.CurrentUser(c)
	metadata := map[string]any{
		"type":           "openai-compatible",
		"label":          name,
		"base_url":       baseURL,
		"api_key":        apiKey,
		"compat_name":    "hub-" + ch.ID + "-" + name,
		"models":         models,
		"owner":          ch.Owner,
		"channel_id":     ch.ID,
		"channel_prefix": ch.Prefix,
		"provider_name":  name,
		"prefix":         pinned,
		"published":      ch.Published,
		"created_by":     actor.Username,
	}
	if proxy := strings.TrimSpace(body.ProxyURL); proxy != "" {
		metadata["proxy_url"] = proxy
	}
	fileName := pinned + ".json"
	if existing, exists := h.authManager.GetByID(h.authIDForPath(filepath.Join(h.cfg.AuthDir, fileName))); exists && existing.ChannelID() != ch.ID {
		hubError(c, multiuser.ErrDenied)
		return
	}
	record := &coreauth.Auth{
		ID:       h.authIDForPath(filepath.Join(h.cfg.AuthDir, fileName)),
		FileName: fileName,
		Provider: "openai-compatible",
		Label:    name,
		Prefix:   pinned,
		Status:   coreauth.StatusActive,
		Metadata: metadata,
	}
	// No CredentialContext here: providers are stamped above with a named prefix
	// and must not receive a numeric credential index from the OAuth path.
	path, err := h.saveTokenRecord(c.Request.Context(), record)
	if err != nil {
		hubError(c, err)
		return
	}
	a, err := h.buildAuthFromFileData(path, nil)
	if err == nil {
		err = h.upsertAuthRecord(c.Request.Context(), a)
	}
	if err != nil {
		hubError(c, err)
		return
	}
	if h.postAuthPersistHook != nil {
		if err := h.postAuthPersistHook(c.Request.Context(), a); err != nil {
			hubError(c, err)
			return
		}
	}
	c.Set("hubTarget", a.ID)
	c.JSON(201, gin.H{"status": "ok", "provider_id": a.ID, "prefix": pinned})
}

// HubPatchProvider updates a mounted provider. The provider name is immutable
// because it is part of the pinned model prefix; api_key left empty is kept.
func (h *Handler) HubPatchProvider(c *gin.Context) {
	a, ok := h.hubCredential(c, true)
	if !ok {
		return
	}
	c.Set("hubAction", "provider.update")
	if hubCredentialKind(a) != "provider" || coreauth.IsConfigAPIKeyAuth(a) {
		hubError(c, fmt.Errorf("%w: not a channel-mounted AI provider", multiuser.ErrInvalid))
		return
	}
	var body hubProviderBody
	if err := c.ShouldBindJSON(&body); err != nil {
		hubError(c, multiuser.ErrInvalid)
		return
	}
	if body.Label == nil && body.BaseURL == nil && body.APIKey == nil && body.ProxyURL == nil && body.Models == nil && body.Disabled == nil {
		hubError(c, multiuser.ErrInvalid)
		return
	}
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	if a.Attributes == nil {
		a.Attributes = map[string]string{}
	}
	if body.Label != nil {
		a.Label = strings.TrimSpace(*body.Label)
		a.Metadata["label"] = a.Label
	}
	if body.BaseURL != nil {
		baseURL, err := normalizeHubProviderBaseURL(*body.BaseURL)
		if err != nil {
			hubError(c, err)
			return
		}
		a.Metadata["base_url"] = baseURL
		a.Attributes["base_url"] = baseURL
	}
	if key := strings.TrimSpace(derefString(body.APIKey)); key != "" {
		a.Metadata["api_key"] = key
		a.Attributes["api_key"] = key
	}
	if body.ProxyURL != nil {
		proxy := strings.TrimSpace(*body.ProxyURL)
		if proxy == "" {
			delete(a.Metadata, "proxy_url")
			a.ProxyURL = ""
		} else {
			a.Metadata["proxy_url"] = proxy
			a.ProxyURL = proxy
		}
	}
	if body.Models != nil {
		models, err := buildHubProviderModels(*body.Models)
		if err != nil {
			hubError(c, err)
			return
		}
		a.Metadata["models"] = models
	}
	if body.Disabled != nil {
		a.Disabled = *body.Disabled
		a.Metadata["disabled"] = a.Disabled
		if a.Disabled {
			a.Status = coreauth.StatusDisabled
		} else {
			a.Status = coreauth.StatusActive
		}
	}
	if err := h.persistHubCredential(c.Request.Context(), a); err != nil {
		hubError(c, err)
		return
	}
	c.JSON(200, gin.H{"status": "ok"})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
