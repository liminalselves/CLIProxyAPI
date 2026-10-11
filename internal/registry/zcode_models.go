package registry

// GetZCodeModels returns the personal Coding Plan models enabled by ZCode's built-in catalog.
func GetZCodeModels() []*ModelInfo {
	return []*ModelInfo{
		{ID: "glm-5.3", Object: "model", OwnedBy: "zhipu", Type: "zcode", DisplayName: "GLM-5.3", ContextLength: 1000000, MaxCompletionTokens: 128000},
		{ID: "glm-5.3-flash", Object: "model", OwnedBy: "zhipu", Type: "zcode", DisplayName: "GLM-5.3 Flash", ContextLength: 1000000, MaxCompletionTokens: 128000},
		{ID: "glm-5.2", Object: "model", OwnedBy: "zhipu", Type: "zcode", DisplayName: "GLM-5.2", ContextLength: 1000000, MaxCompletionTokens: 128000},
		{ID: "glm-5-turbo", Object: "model", OwnedBy: "zhipu", Type: "zcode", DisplayName: "GLM-5 Turbo", ContextLength: 200000, MaxCompletionTokens: 64000},
	}
}
