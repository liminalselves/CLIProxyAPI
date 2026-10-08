package cliproxy

import (
	"strings"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
)

func (s *Service) channelModels(a *coreauth.Auth, models []*ModelInfo) []*ModelInfo {
	s.cfgMu.RLock()
	enabled := s.cfg != nil && s.cfg.MultiUser.Enabled
	s.cfgMu.RUnlock()
	return filterChannelModels(a, models, enabled)
}

func filterChannelModels(a *coreauth.Auth, models []*ModelInfo, enabled bool) []*ModelInfo {
	if !enabled {
		return models
	}
	if !a.ChannelPublished() {
		return nil
	}
	out := make([]*ModelInfo, 0, len(models))
	for _, model := range models {
		if model == nil || !strings.HasPrefix(model.ID, a.Prefix+"/") || !a.ChannelModelEnabled(model.ID) {
			continue
		}
		out = append(out, model)
	}
	return out
}
