package handlers

import (
	"github.com/gin-gonic/gin"
	"maps"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/registry"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

// FilterModelsForRequest keeps published routes available to the key and derives
// bare entries only from the remaining routes, without changing the shared registry.
func (h *BaseAPIHandler) FilterModelsForRequest(c *gin.Context, models []map[string]any) []map[string]any {
	if h == nil || h.Cfg == nil || !h.Cfg.RequireChannelModel {
		return models
	}
	ctx := c.Request.Context()
	allowedRoutes := make(map[string]struct{})
	if h.AuthManager != nil {
		reg := registry.GetGlobalRegistry()
		for _, credential := range h.AuthManager.List() {
			if credential.Disabled || credential.Status == coreauth.StatusDisabled || !credential.ChannelPublished() {
				continue
			}
			for _, model := range reg.GetModelsForClient(credential.ID) {
				if model == nil || !strings.HasPrefix(model.ID, credential.Prefix+"/") || !credential.ChannelModelEnabled(model.ID) || coreauth.AuthModelBlocked(ctx, credential, model.ID) {
					continue
				}
				allowedRoutes[model.ID] = struct{}{}
			}
		}
	}
	result := make([]map[string]any, 0, len(models))
	seenBare := make(map[string]struct{})
	for _, model := range models {
		idField := "id"
		id, _ := model[idField].(string)
		if id == "" {
			idField = "name"
			id, _ = model[idField].(string)
			id = strings.TrimPrefix(id, "models/")
		}
		if _, allowed := allowedRoutes[id]; !allowed {
			continue
		}
		result = append(result, model)
		if !coreauth.BareModelsAllowed(ctx) {
			continue
		}
		_, base, pinned := strings.Cut(id, "/")
		if !pinned {
			continue
		}
		if _, exists := seenBare[base]; exists {
			continue
		}
		seenBare[base] = struct{}{}
		bare := maps.Clone(model)
		bare[idField] = base
		for _, field := range []string{"display_name", "displayName", "description"} {
			if bare[field] == id || bare[field] == "models/"+id {
				bare[field] = base
			}
		}
		result = append(result, bare)
	}
	return result
}
