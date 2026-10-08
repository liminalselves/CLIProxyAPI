package management

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/multiuser"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// MigrateHubNaming upgrades pre-pinning hub data to the current naming scheme:
//   - channel prefixes move from "username-slug" to the site-wide unique slug;
//   - credentials attached to a channel receive a per-(channel, type) index and
//     a pinned routing prefix "slug-type#n".
//
// It runs once at hub startup after the auth manager has loaded existing files,
// is idempotent, and never renames credential files (auth IDs stay stable).
func (h *Handler) MigrateHubNaming() {
	if h.hubStore == nil || h.authManager == nil {
		return
	}
	h.migrateHubChannelPrefixes()
	h.migrateHubCredentialPrefixes()
}

func (h *Handler) migrateHubChannelPrefixes() {
	channels := h.hubStore.Channels()
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].CreatedAt.Equal(channels[j].CreatedAt) {
			return channels[i].ID < channels[j].ID
		}
		return channels[i].CreatedAt.Before(channels[j].CreatedAt)
	})
	for _, ch := range channels {
		if ch.Prefix == ch.Slug {
			continue
		}
		target := ch.Slug
		if err := h.hubStore.UpdateChannelPrefix(ch.ID, target); err != nil {
			// Another channel already owns this slug: fall back to the previous
			// composite prefix, which is unique by construction.
			target = ch.Prefix
			if errFallback := h.hubStore.UpdateChannelPrefix(ch.ID, target); errFallback != nil {
				log.WithError(errFallback).Errorf("multi-user: could not migrate channel %s prefix", ch.ID)
				continue
			}
		}
		if target != ch.Slug {
			log.Warnf("multi-user: channel %s kept legacy prefix %q because slug %q is taken", ch.ID, target, ch.Slug)
		} else {
			log.Infof("multi-user: channel %s prefix migrated to %q", ch.ID, target)
		}
	}
}

func (h *Handler) migrateHubCredentialPrefixes() {
	// Group attached credentials by channel so indices are assigned in a
	// deterministic order (by auth ID) within each (channel, type) bucket.
	byChannel := map[string][]*coreauth.Auth{}
	for _, a := range h.authManager.List() {
		if a == nil || a.ChannelID() == "" {
			continue
		}
		byChannel[a.ChannelID()] = append(byChannel[a.ChannelID()], a)
	}
	for channelID, auths := range byChannel {
		ch, ok := h.hubStore.Channel(channelID)
		if !ok {
			log.Warnf("multi-user: %d credentials reference missing channel %s; skipped", len(auths), channelID)
			continue
		}
		sort.Slice(auths, func(i, j int) bool { return auths[i].ID < auths[j].ID })
		floors := map[string]int{}
		for _, a := range auths {
			credType := strings.ToLower(strings.TrimSpace(a.Provider))
			if credType == "" {
				continue
			}
			index, hasIndex := hubMetadataInt(a.Metadata, "channel_index")
			if !hasIndex || index <= 0 {
				index = floors[credType] + 1
			}
			if floors[credType] < index {
				floors[credType] = index
			}
			pinned := hubPinnedPrefix(ch, credType, index, a)
			if a.Prefix == pinned && hasIndex {
				continue
			}
			if a.Metadata == nil {
				a.Metadata = map[string]any{}
			}
			a.Metadata["channel_index"] = index
			a.Metadata["credential_type"] = credType
			a.Metadata["channel_prefix"] = ch.Prefix
			a.Metadata["prefix"] = pinned
			if _, okCreated := a.Metadata["created_by"]; !okCreated {
				a.Metadata["created_by"] = a.Owner()
			}
			a.Prefix = pinned
			if err := h.persistHubCredential(context.Background(), a); err != nil {
				log.WithError(err).Errorf("multi-user: could not migrate credential %s prefix", a.ID)
				continue
			}
			log.Infof("multi-user: credential %s pinned as %s", a.ID, pinned)
		}
		for credType, floor := range floors {
			if err := h.hubStore.SetCredentialIndexFloor(ch.ID, credType, floor); err != nil {
				log.WithError(err).Errorf("multi-user: could not set index floor for channel %s", ch.ID)
			}
		}
	}
}

// hubPinnedPrefix computes the routing prefix of a credential: providers keep
// their named prefix ("slug-provider"), OAuth credentials get "slug-type#n".
func hubPinnedPrefix(ch multiuser.Channel, credType string, index int, a *coreauth.Auth) string {
	if a != nil {
		if name, _ := a.Metadata["provider_name"].(string); strings.TrimSpace(name) != "" {
			return ch.Prefix + "-" + strings.TrimSpace(name)
		}
	}
	return fmt.Sprintf("%s-%s#%d", ch.Prefix, credType, index)
}
