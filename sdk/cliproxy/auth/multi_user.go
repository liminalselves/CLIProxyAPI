package auth

import (
	"context"
	"strings"
)

func (a *Auth) Owner() string     { return a.metadataString("owner") }
func (a *Auth) ChannelID() string { return a.metadataString("channel_id") }

func (a *Auth) metadataString(key string) string {
	if a == nil || a.Metadata == nil {
		return ""
	}
	value, _ := a.Metadata[key].(string)
	return strings.TrimSpace(value)
}

func (a *Auth) ChannelPublished() bool {
	if a == nil || a.Owner() == "" || a.ChannelID() == "" || a.Prefix == "" {
		return false
	}
	published, ok := a.Metadata["published"].(bool)
	return !ok || published
}

// ChannelModelEnabled uses an explicit allowlist only when it is present.
func (a *Auth) ChannelModelEnabled(model string) bool {
	if a == nil {
		return false
	}
	raw, exists := a.Metadata["enabled_models"]
	if !exists {
		return true
	}
	base := strings.TrimPrefix(model, a.Prefix+"/")
	switch list := raw.(type) {
	case []any:
		for _, value := range list {
			if value == base {
				return true
			}
		}
	case []string:
		for _, value := range list {
			if value == base {
				return true
			}
		}
	}
	return false
}

type bareModelsContextKey struct{}

// WithBareModelsAllowed marks a request as permitted to call bare base model
// names in multi-user mode. Callers spread evenly across every published
// credential serving that model, with failover until all are tried.
func WithBareModelsAllowed(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, bareModelsContextKey{}, true)
}

// BareModelsAllowed reports whether the request carries the bare-model grant.
func BareModelsAllowed(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	allowed, _ := ctx.Value(bareModelsContextKey{}).(bool)
	return allowed
}
