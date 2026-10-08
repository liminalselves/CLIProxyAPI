package multiuser

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalid      = errors.New("invalid input")
	ErrDenied       = errors.New("permission denied")
	ErrConflict     = errors.New("already exists")
	ErrNotFound     = errors.New("not found")
	usernamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,31}$`)
	channelPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
)

type User struct {
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at"`
}

type userRecord struct {
	User
	PasswordHash string `json:"password_hash"`
}

type APIKey struct {
	ID        string     `json:"id"`
	Owner     string     `json:"owner"`
	Name      string     `json:"name"`
	Hint      string     `json:"hint"`
	Disabled  bool       `json:"disabled"`
	CreatedAt time.Time  `json:"created_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

type keyRecord struct {
	APIKey
	Hash string `json:"hash"`
}

type Invite struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	MaxUses   int       `json:"max_uses"`
	UsedBy    []string  `json:"used_by"`
	Revoked   bool      `json:"revoked"`
}

type Channel struct {
	ID                    string         `json:"id"`
	Owner                 string         `json:"owner"`
	Slug                  string         `json:"slug"`
	Prefix                string         `json:"prefix"`
	Published             bool           `json:"published"`
	Public                bool           `json:"public"`
	Announcement          string         `json:"announcement,omitempty"`
	AnnouncementUpdatedAt time.Time      `json:"announcement_updated_at,omitempty"`
	NextCredentialIndex   map[string]int `json:"next_credential_index,omitempty"`
	CreatedAt             time.Time      `json:"created_at"`
}

// BoardMessage is one plain-text post on a channel's message board.
type BoardMessage struct {
	ID        string    `json:"id"`
	ChannelID string    `json:"channel_id"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	maxAnnouncementRunes = 5000
	maxBoardMessageRunes = 2000
	boardMessagesPerChan = 200
)

type session struct {
	Owner     string    `json:"owner"`
	ExpiresAt time.Time `json:"expires_at"`
}

type state struct {
	Users    map[string]userRecord   `json:"users"`
	Keys     map[string]keyRecord    `json:"keys"`
	Invites  map[string]Invite       `json:"invites"`
	Sessions map[string]session      `json:"sessions"`
	Channels map[string]Channel      `json:"channels"`
	Messages map[string]BoardMessage `json:"messages"`
}

type Store struct {
	mu         sync.RWMutex
	data       state
	dir        string
	sessionTTL time.Duration
	now        func() time.Time
	logMu      sync.Mutex
}

func Open(dir string, sessionTTL time.Duration) (*Store, error) {
	if sessionTTL <= 0 {
		sessionTTL = 7 * 24 * time.Hour
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create account directory: %w", err)
	}
	s := &Store{dir: dir, sessionTTL: sessionTTL, now: time.Now}
	s.data = state{Users: map[string]userRecord{}, Keys: map[string]keyRecord{}, Invites: map[string]Invite{}, Sessions: map[string]session{}, Channels: map[string]Channel{}, Messages: map[string]BoardMessage{}}
	raw, err := os.ReadFile(filepath.Join(dir, "accounts.db"))
	if err == nil {
		if errDecode := json.Unmarshal(raw, &s.data); errDecode != nil {
			return nil, fmt.Errorf("read accounts: %w", errDecode)
		}
		if s.data.Users == nil || s.data.Keys == nil || s.data.Invites == nil || s.data.Sessions == nil || s.data.Channels == nil {
			return nil, fmt.Errorf("accounts database is incomplete")
		}
		if s.data.Messages == nil {
			s.data.Messages = map[string]BoardMessage{}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read accounts: %w", err)
	}
	return s, nil
}

func randomToken(prefix string) (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b[:]), nil
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func (s *Store) mutate(fn func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	var next state
	if err = json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	raw, err = json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	if err = atomicWrite(filepath.Join(s.dir, "accounts.db"), raw); err != nil {
		return fmt.Errorf("save accounts: %w", err)
	}
	s.data = next
	return nil
}

func atomicWrite(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".accounts-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() { _ = os.Remove(name) }()
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	errClose := file.Close()
	if err != nil {
		return err
	}
	if errClose != nil {
		return errClose
	}
	return os.Rename(name, path)
}

func (s *Store) SiteID() string { return hashToken(s.dir) }

func (s *Store) NeedsSetup() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Users) == 0
}

func validatePassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("%w: password must contain 8 to 72 bytes", ErrInvalid)
	}
	return nil
}

func (s *Store) Register(username, password, code string) (User, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) {
		return User{}, fmt.Errorf("%w: username must be 3 to 32 lowercase letters, digits or underscores, starting with a letter", ErrInvalid)
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := User{Username: username, Role: "member", CreatedAt: s.now().UTC()}
	err = s.mutate(func(d *state) error {
		if _, exists := d.Users[username]; exists {
			return ErrConflict
		}
		if len(d.Users) == 0 {
			user.Role = "admin"
		} else {
			found := false
			for id, invite := range d.Invites {
				if invite.Code != strings.TrimSpace(code) {
					continue
				}
				if invite.Revoked || !s.now().Before(invite.ExpiresAt) || len(invite.UsedBy) >= invite.MaxUses {
					return fmt.Errorf("%w: invite is expired or exhausted", ErrInvalid)
				}
				invite.UsedBy = append(invite.UsedBy, username)
				d.Invites[id] = invite
				found = true
				break
			}
			if !found {
				return fmt.Errorf("%w: a valid invite is required", ErrInvalid)
			}
		}
		d.Users[username] = userRecord{User: user, PasswordHash: string(hash)}
		return nil
	})
	return user, err
}

func (s *Store) Login(username, password string) (User, string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	s.mu.RLock()
	record, ok := s.data.Users[username]
	s.mu.RUnlock()
	if !ok || record.Disabled || bcrypt.CompareHashAndPassword([]byte(record.PasswordHash), []byte(password)) != nil {
		return User{}, "", ErrDenied
	}
	token, err := randomToken("session-")
	if err != nil {
		return User{}, "", err
	}
	err = s.mutate(func(d *state) error {
		current, exists := d.Users[username]
		if !exists || current.Disabled || current.PasswordHash != record.PasswordHash {
			return ErrDenied
		}
		for hash, ses := range d.Sessions {
			if !s.now().Before(ses.ExpiresAt) {
				delete(d.Sessions, hash)
			}
		}
		d.Sessions[hashToken(token)] = session{Owner: username, ExpiresAt: s.now().Add(s.sessionTTL)}
		return nil
	})
	return record.User, token, err
}

func (s *Store) AuthenticateSession(token string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ses, ok := s.data.Sessions[hashToken(token)]
	if !ok || !s.now().Before(ses.ExpiresAt) {
		return User{}, false
	}
	user, ok := s.data.Users[ses.Owner]
	return user.User, ok && !user.Disabled
}

func (s *Store) Logout(token string) error {
	return s.mutate(func(d *state) error { delete(d.Sessions, hashToken(token)); return nil })
}

func (s *Store) User(username string) (User, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.data.Users[username]
	return u.User, ok
}

func (s *Store) Users() []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]User, 0, len(s.data.Users))
	for _, u := range s.data.Users {
		out = append(out, u.User)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out
}

func (s *Store) UpdateUser(actor User, username string, disabled *bool, role *string) error {
	return s.mutate(func(d *state) error {
		if !isAdmin(d, actor) {
			return ErrDenied
		}
		u, ok := d.Users[username]
		if !ok {
			return ErrNotFound
		}
		if disabled != nil || role != nil {
			if disabled != nil {
				u.Disabled = *disabled
			}
			if role != nil {
				if *role != "admin" && *role != "member" {
					return ErrInvalid
				}
				u.Role = *role
			}
		}
		d.Users[username] = u
		admins := 0
		for _, user := range d.Users {
			if user.Role == "admin" && !user.Disabled {
				admins++
			}
		}
		if admins == 0 {
			return fmt.Errorf("%w: at least one active administrator is required", ErrInvalid)
		}
		if u.Disabled {
			for hash, ses := range d.Sessions {
				if ses.Owner == username {
					delete(d.Sessions, hash)
				}
			}
		}
		return nil
	})
}

func (s *Store) ChangePassword(actor User, current, password string) error {
	if err := validatePassword(password); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.mutate(func(d *state) error {
		u, ok := d.Users[actor.Username]
		if !ok || u.Disabled || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(current)) != nil {
			return ErrDenied
		}
		u.PasswordHash = string(hash)
		d.Users[actor.Username] = u
		for key, ses := range d.Sessions {
			if ses.Owner == actor.Username {
				delete(d.Sessions, key)
			}
		}
		return nil
	})
}

func isAdmin(d *state, actor User) bool {
	u, ok := d.Users[actor.Username]
	return ok && !u.Disabled && u.Role == "admin"
}

// IsValidSlug reports whether value is a valid channel slug or AI provider name.
// Slugs are lowercase letters/digits/hyphens and never contain '#' or '/', which
// keeps pinned model prefixes ("slug-type#n", "slug-provider") unambiguous.
func IsValidSlug(value string) bool {
	return channelPattern.MatchString(value)
}

func (s *Store) CreateKey(actor User, name string) (APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 64 {
		return APIKey{}, "", ErrInvalid
	}
	secret, err := randomToken("sk-hub-")
	if err != nil {
		return APIKey{}, "", err
	}
	id, err := randomToken("key-")
	if err != nil {
		return APIKey{}, "", err
	}
	key := APIKey{ID: id, Owner: actor.Username, Name: name, Hint: secret[:10] + "…" + secret[len(secret)-4:], CreatedAt: s.now().UTC()}
	err = s.mutate(func(d *state) error {
		u, ok := d.Users[actor.Username]
		if !ok || u.Disabled {
			return ErrDenied
		}
		d.Keys[id] = keyRecord{APIKey: key, Hash: hashToken(secret)}
		return nil
	})
	return key, secret, err
}

func (s *Store) AuthenticateKey(secret string) (User, APIKey, bool) {
	hash := hashToken(secret)
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, key := range s.data.Keys {
		if key.Hash != hash || key.Disabled || key.RevokedAt != nil {
			continue
		}
		u, ok := s.data.Users[key.Owner]
		return u.User, key.APIKey, ok && !u.Disabled
	}
	return User{}, APIKey{}, false
}

func (s *Store) Keys(actor User) []APIKey {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]APIKey, 0)
	for _, key := range s.data.Keys {
		if key.Owner == actor.Username && key.RevokedAt == nil {
			out = append(out, key.APIKey)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) UpdateKey(actor User, id string, name *string, disabled *bool, revoke bool) error {
	return s.mutate(func(d *state) error {
		key, ok := d.Keys[id]
		if !ok || key.RevokedAt != nil {
			return ErrNotFound
		}
		if key.Owner != actor.Username && !isAdmin(d, actor) {
			return ErrDenied
		}
		if name != nil {
			value := strings.TrimSpace(*name)
			if value == "" || len([]rune(value)) > 64 {
				return ErrInvalid
			}
			key.Name = value
		}
		if disabled != nil {
			key.Disabled = *disabled
		}
		if revoke {
			now := s.now().UTC()
			key.RevokedAt = &now
			key.Disabled = true
		}
		d.Keys[id] = key
		return nil
	})
}

func (s *Store) CreateInvite(actor User, maxUses, days int) (Invite, error) {
	if maxUses < 1 || maxUses > 100 || days < 1 || days > 365 {
		return Invite{}, ErrInvalid
	}
	code, err := randomToken("invite-")
	if err != nil {
		return Invite{}, err
	}
	id, err := randomToken("inv-")
	if err != nil {
		return Invite{}, err
	}
	invite := Invite{ID: id, Code: code, CreatedBy: actor.Username, CreatedAt: s.now().UTC(), ExpiresAt: s.now().UTC().AddDate(0, 0, days), MaxUses: maxUses, UsedBy: []string{}}
	err = s.mutate(func(d *state) error {
		if !isAdmin(d, actor) {
			return ErrDenied
		}
		d.Invites[id] = invite
		return nil
	})
	return invite, err
}

func (s *Store) Invites(actor User) ([]Invite, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !isAdmin(&s.data, actor) {
		return nil, ErrDenied
	}
	out := make([]Invite, 0, len(s.data.Invites))
	for _, invite := range s.data.Invites {
		invite.UsedBy = append([]string{}, invite.UsedBy...)
		out = append(out, invite)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *Store) RevokeInvite(actor User, id string) error {
	return s.mutate(func(d *state) error {
		if !isAdmin(d, actor) {
			return ErrDenied
		}
		invite, ok := d.Invites[id]
		if !ok {
			return ErrNotFound
		}
		invite.Revoked = true
		d.Invites[id] = invite
		return nil
	})
}

func (s *Store) CreateChannel(actor User, slug string, public bool) (Channel, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !channelPattern.MatchString(slug) {
		return Channel{}, ErrInvalid
	}
	id, err := randomToken("ch-")
	if err != nil {
		return Channel{}, err
	}
	// The prefix is the slug itself: channel slugs are unique site-wide and model
	// IDs no longer embed the owner's username.
	channel := Channel{ID: id, Owner: actor.Username, Slug: slug, Prefix: slug, Published: true, Public: public, CreatedAt: s.now().UTC()}
	err = s.mutate(func(d *state) error {
		if u, ok := d.Users[actor.Username]; !ok || u.Disabled {
			return ErrDenied
		}
		for _, ch := range d.Channels {
			if ch.Slug == slug || ch.Prefix == slug {
				return ErrConflict
			}
		}
		d.Channels[id] = channel
		return nil
	})
	return channel, err
}

func (s *Store) Channels() []Channel {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Channel, 0, len(s.data.Channels))
	for _, ch := range s.data.Channels {
		out = append(out, ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix < out[j].Prefix })
	return out
}

func (s *Store) Channel(id string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.data.Channels[id]
	return ch, ok
}

// canManageChannel reports whether the actor may manage a channel's content
// (credentials, providers, announcement, board moderation). Public channels are
// managed by every signed-in member; the public flag itself stays owner/admin only.
func canManageChannel(d *state, ch Channel, actor User) bool {
	if ch.Owner == actor.Username || isAdmin(d, actor) {
		return true
	}
	if !ch.Public {
		return false
	}
	u, ok := d.Users[actor.Username]
	return ok && !u.Disabled
}

// DeleteChannel removes a channel and its board messages. Only the owner or an
// administrator may delete; the handler layer must ensure no credentials are
// still attached before calling.
func (s *Store) DeleteChannel(actor User, id string) error {
	return s.mutate(func(d *state) error {
		ch, ok := d.Channels[id]
		if !ok {
			return ErrNotFound
		}
		if ch.Owner != actor.Username && !isAdmin(d, actor) {
			return ErrDenied
		}
		delete(d.Channels, id)
		for messageID, message := range d.Messages {
			if message.ChannelID == id {
				delete(d.Messages, messageID)
			}
		}
		return nil
	})
}

// CanManageChannel exposes the content-management check for hub handlers that
// operate on credentials attached to a channel.
func (s *Store) CanManageChannel(actor User, channelID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ch, ok := s.data.Channels[channelID]
	if !ok {
		return false
	}
	return canManageChannel(&s.data, ch, actor)
}

func (s *Store) UpdateChannel(actor User, id string, published *bool, announcement *string, public *bool) (Channel, error) {
	var updated Channel
	err := s.mutate(func(d *state) error {
		ch, ok := d.Channels[id]
		if !ok {
			return ErrNotFound
		}
		if !canManageChannel(d, ch, actor) {
			return ErrDenied
		}
		if published != nil {
			ch.Published = *published
		}
		if announcement != nil {
			value := strings.TrimSpace(*announcement)
			if len([]rune(value)) > maxAnnouncementRunes {
				return ErrInvalid
			}
			ch.Announcement = value
			ch.AnnouncementUpdatedAt = s.now().UTC()
		}
		if public != nil {
			if ch.Owner != actor.Username && !isAdmin(d, actor) {
				return ErrDenied
			}
			ch.Public = *public
		}
		d.Channels[id] = ch
		updated = ch
		return nil
	})
	return updated, err
}

// AllocateCredentialIndex returns the next per-(channel, credential type) index
// used to build pinned credential prefixes ("slug-type#n"). Indices are monotonic
// and never reused so pinned model IDs stay stable after deletions.
func (s *Store) AllocateCredentialIndex(channelID, credentialType string) (int, error) {
	credentialType = strings.ToLower(strings.TrimSpace(credentialType))
	if credentialType == "" {
		return 0, ErrInvalid
	}
	index := 0
	err := s.mutate(func(d *state) error {
		ch, ok := d.Channels[channelID]
		if !ok {
			return ErrNotFound
		}
		if ch.NextCredentialIndex == nil {
			ch.NextCredentialIndex = map[string]int{}
		}
		next := ch.NextCredentialIndex[credentialType] + 1
		ch.NextCredentialIndex[credentialType] = next
		d.Channels[channelID] = ch
		index = next
		return nil
	})
	return index, err
}

// SetCredentialIndexFloor raises the per-(channel, type) counter to at least the
// given value. Used by startup migration after scanning existing credentials.
func (s *Store) SetCredentialIndexFloor(channelID, credentialType string, floor int) error {
	credentialType = strings.ToLower(strings.TrimSpace(credentialType))
	if credentialType == "" || floor < 0 {
		return ErrInvalid
	}
	return s.mutate(func(d *state) error {
		ch, ok := d.Channels[channelID]
		if !ok {
			return ErrNotFound
		}
		if ch.NextCredentialIndex == nil {
			ch.NextCredentialIndex = map[string]int{}
		}
		if ch.NextCredentialIndex[credentialType] < floor {
			ch.NextCredentialIndex[credentialType] = floor
			d.Channels[channelID] = ch
		}
		return nil
	})
}

// UpdateChannelPrefix rewrites a channel's slug/prefix during startup migration
// to the site-wide slug scheme. It is not exposed over HTTP.
func (s *Store) UpdateChannelPrefix(id, slug string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if !channelPattern.MatchString(slug) {
		return ErrInvalid
	}
	return s.mutate(func(d *state) error {
		ch, ok := d.Channels[id]
		if !ok {
			return ErrNotFound
		}
		for otherID, other := range d.Channels {
			if otherID != id && (other.Slug == slug || other.Prefix == slug) {
				return ErrConflict
			}
		}
		ch.Slug = slug
		ch.Prefix = slug
		d.Channels[id] = ch
		return nil
	})
}

func (s *Store) AddBoardMessage(actor User, channelID, content string) (BoardMessage, error) {
	content = strings.TrimSpace(content)
	runes := []rune(content)
	if len(runes) == 0 || len(runes) > maxBoardMessageRunes {
		return BoardMessage{}, ErrInvalid
	}
	id, err := randomToken("msg-")
	if err != nil {
		return BoardMessage{}, err
	}
	message := BoardMessage{ID: id, ChannelID: channelID, Author: actor.Username, Content: content, CreatedAt: s.now().UTC()}
	err = s.mutate(func(d *state) error {
		if u, ok := d.Users[actor.Username]; !ok || u.Disabled {
			return ErrDenied
		}
		if _, exists := d.Channels[channelID]; !exists {
			return ErrNotFound
		}
		d.Messages[id] = message
		// Prune oldest messages beyond the per-channel cap.
		owned := make([]BoardMessage, 0, boardMessagesPerChan+1)
		for _, m := range d.Messages {
			if m.ChannelID == channelID {
				owned = append(owned, m)
			}
		}
		if len(owned) > boardMessagesPerChan {
			sort.Slice(owned, func(i, j int) bool { return owned[i].CreatedAt.Before(owned[j].CreatedAt) })
			for _, m := range owned[:len(owned)-boardMessagesPerChan] {
				delete(d.Messages, m.ID)
			}
		}
		return nil
	})
	return message, err
}

// ListBoardMessages returns newest-first messages for a channel. beforeID acts
// as a cursor: when set, only messages older than it are returned.
func (s *Store) ListBoardMessages(channelID string, limit int, beforeID string) []BoardMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BoardMessage, 0, 64)
	for _, m := range s.data.Messages {
		if m.ChannelID == channelID {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if beforeID != "" {
		cut := -1
		for i, m := range out {
			if m.ID == beforeID {
				cut = i
				break
			}
		}
		if cut < 0 {
			return []BoardMessage{}
		}
		out = out[cut+1:]
	}
	if limit <= 0 || limit > boardMessagesPerChan {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) DeleteBoardMessage(actor User, channelID, messageID string) error {
	return s.mutate(func(d *state) error {
		message, ok := d.Messages[messageID]
		if !ok || message.ChannelID != channelID {
			return ErrNotFound
		}
		ch, ok := d.Channels[channelID]
		if !ok {
			return ErrNotFound
		}
		if message.Author != actor.Username && !canManageChannel(d, ch, actor) {
			return ErrDenied
		}
		delete(d.Messages, messageID)
		return nil
	})
}
