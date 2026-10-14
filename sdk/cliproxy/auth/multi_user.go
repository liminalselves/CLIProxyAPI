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

type blockedModelsContextKey struct{}
type modelBlocklist map[string]struct{}

// WithBlockedModels attaches the key's model exclusions to this request.
// Entries may be a bare base model ("gpt-6", excluded on every route), a
// pinned "prefix/model" route, a credential or provider prefix
// ("123-kimi#1", "123-myprovider"), or a channel slug ("123").
func WithBlockedModels(ctx context.Context, models []string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	blocked := make(modelBlocklist, len(models))
	for _, model := range models {
		if model = canonicalModelKey(model); model != "" {
			blocked[model] = struct{}{}
		}
	}
	return context.WithValue(ctx, blockedModelsContextKey{}, blocked)
}

func blockedModelsFromContext(ctx context.Context) modelBlocklist {
	if ctx == nil {
		return nil
	}
	blocked, _ := ctx.Value(blockedModelsContextKey{}).(modelBlocklist)
	return blocked
}

// WithModelRestrictionsFromContext preserves the key policy when handlers replace the request context.
func WithModelRestrictionsFromContext(ctx, source context.Context) context.Context {
	if blocked := blockedModelsFromContext(source); blocked != nil {
		return context.WithValue(ctx, blockedModelsContextKey{}, blocked)
	}
	return ctx
}

func (blocked modelBlocklist) blocks(model string) bool {
	model = canonicalModelKey(model)
	if _, exists := blocked[model]; exists {
		return true
	}
	if _, base, pinned := strings.Cut(model, "/"); pinned {
		_, exists := blocked[base]
		return exists
	}
	return false
}

// blocksAuth reports whether this credential is excluded for the model. On top
// of the string-level rules of blocks, entries scope to the credential by its
// pinned prefix ("123-kimi#1"), by channel plus credential type ("123-kimi"),
// or by the whole channel slug ("123"), matched against channel metadata.
func (blocked modelBlocklist) blocksAuth(auth *Auth, model string) bool {
	if len(blocked) == 0 || auth == nil {
		return false
	}
	if blocked.blocks(model) {
		return true
	}
	prefix := strings.TrimSpace(auth.Prefix)
	base := canonicalModelKey(model)
	if route, rest, pinned := strings.Cut(base, "/"); pinned {
		if route != prefix {
			// Pinned to another credential; this auth stays eligible.
			return false
		}
		base = rest
	}
	if prefix != "" {
		if _, ok := blocked[prefix]; ok {
			return true
		}
		if _, ok := blocked[prefix+"/"+base]; ok {
			return true
		}
	}
	channel := auth.metadataString("channel_prefix")
	if channel == "" {
		return false
	}
	if _, ok := blocked[channel]; ok {
		return true
	}
	if credType := auth.metadataString("credential_type"); credType != "" {
		_, ok := blocked[channel+"-"+credType]
		return ok
	}
	return false
}

// ModelBlocked matches either a pinned route or a base name excluded across all channels.
func ModelBlocked(ctx context.Context, model string) bool {
	return blockedModelsFromContext(ctx).blocks(model)
}

// AuthModelBlocked reports whether this credential's route for the model is
// excluded for the request key, including credential and channel scopes.
func AuthModelBlocked(ctx context.Context, auth *Auth, model string) bool {
	return blockedModelsFromContext(ctx).blocksAuth(auth, model)
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
