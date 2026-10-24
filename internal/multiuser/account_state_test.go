package multiuser

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// gatedPersist returns a persist hook that signals every call on entered and
// then parks until release is closed, letting tests hold the store inside the
// persistence step deterministically (no sleeps, no filesystem tricks).
func gatedPersist(entered chan<- struct{}, release <-chan struct{}) func(string, []byte) error {
	return func(path string, data []byte) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return atomicWrite(path, data)
	}
}

// TestReadsRunDuringPersistenceAndSeeCommittedState proves that while a
// mutation is parked inside disk persistence (holding the dedicated write
// mutex), readers neither block on the write pipeline nor observe the
// in-flight, not-yet-published state.
func TestReadsRunDuringPersistenceAndSeeCommittedState(t *testing.T) {
	s, admin, bob := testAccounts(t)
	entered := make(chan struct{}, 4)
	release := make(chan struct{})
	s.persist = gatedPersist(entered, release)

	disabled := true
	done := make(chan error, 1)
	go func() { done <- s.UpdateUser(admin, bob.Username, &disabled, nil) }()
	<-entered // the mutation is now parked inside persistence, before publish

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		if u, ok := s.User(bob.Username); !ok || u.Disabled {
			t.Errorf("read saw uncommitted state: %+v (ok=%v)", u, ok)
		}
		if len(s.Users()) != 2 {
			t.Errorf("Users() blocked or inconsistent during persistence")
		}
		if _, _, ok := s.AuthenticateKey("sk-hub-definitely-unknown"); ok {
			t.Errorf("unknown key authenticated during persistence")
		}
	}()
	select {
	case <-readDone:
	case <-time.After(5 * time.Second):
		t.Fatal("read blocked while a mutation was persisting")
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if u, ok := s.User(bob.Username); !ok || !u.Disabled {
		t.Fatalf("mutation not published after persistence: %+v (ok=%v)", u, ok)
	}
}

// TestConcurrentMutatesSerializeWithoutLostUpdates fires many concurrent
// mutations; the dedicated write mutex must serialize them so every increment
// lands on the latest committed state and none is lost.
func TestConcurrentMutatesSerializeWithoutLostUpdates(t *testing.T) {
	s, admin, _ := testAccounts(t)
	ch, err := s.CreateChannel(admin, "count", false)
	if err != nil {
		t.Fatal(err)
	}
	const rounds = 24
	var wg sync.WaitGroup
	errs := make(chan error, rounds)
	for i := 0; i < rounds; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.AllocateCredentialIndex(ch.ID, "kimi")
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if index, err := s.AllocateCredentialIndex(ch.ID, "kimi"); err != nil || index != rounds+1 {
		t.Fatalf("lost updates: index=%d err=%v", index, err)
	}
}

// TestFailedPersistenceRollsBackWithoutAlias proves that a persistence failure
// leaves committed state untouched: invites are not consumed, nested maps and
// slices in the discarded clone never alias committed structures, returned
// values stay clones, and a later publish detaches the previous generation.
func TestFailedPersistenceRollsBackWithoutAlias(t *testing.T) {
	s, admin, bob := testAccounts(t)
	key, secret, err := s.CreateKey(bob, "primary")
	if err != nil {
		t.Fatal(err)
	}
	ch, err := s.CreateChannel(admin, "alias", false)
	if err != nil {
		t.Fatal(err)
	}
	blocked := []string{"gpt-4"}
	if err := s.UpdateKey(bob, key.ID, nil, nil, nil, &blocked, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCredentialIndexFloor(ch.ID, "kimi", 5); err != nil {
		t.Fatal(err)
	}
	invite, err := s.CreateInvite(admin, 1, 7)
	if err != nil {
		t.Fatal(err)
	}

	persistErr := errors.New("disk full")
	s.persist = func(path string, data []byte) error { return persistErr }

	// Public mutations that touch nested structures fail at persistence.
	newName := "renamed"
	newBlocked := []string{"claude"}
	if err := s.UpdateKey(bob, key.ID, &newName, nil, nil, &newBlocked, false); !errors.Is(err, persistErr) {
		t.Fatalf("update key: %v", err)
	}
	if _, err := s.Register("carol", "password-carol", invite.Code); !errors.Is(err, persistErr) {
		t.Fatalf("register: %v", err)
	}

	// A closure that mutates nested maps and slices, then aborts, must only
	// ever have touched a discarded deep copy.
	abort := errors.New("abort")
	inviteID := invite.ID
	if err := s.mutate(func(d *state) error {
		d.Keys[key.ID].BlockedModels[0] = "clobbered"
		d.Channels[ch.ID].NextCredentialIndex["kimi"] = 99
		invite := d.Invites[inviteID]
		invite.UsedBy = append(invite.UsedBy, "ghost")
		d.Invites[inviteID] = invite
		return abort
	}); !errors.Is(err, abort) {
		t.Fatalf("mutate: %v", err)
	}

	// Committed state is untouched, including nested structures and the index.
	keys := s.Keys(bob)
	if len(keys) != 1 || keys[0].Name != "primary" || len(keys[0].BlockedModels) != 1 || keys[0].BlockedModels[0] != "gpt-4" {
		t.Fatalf("failed mutation leaked into committed keys: %+v", keys)
	}
	if _, _, ok := s.AuthenticateKey(secret); !ok {
		t.Fatal("key index corrupted by failed persistence")
	}
	if _, ok := s.User("carol"); ok {
		t.Fatal("failed registration published partial state")
	}
	s.mu.RLock()
	if s.data.Channels[ch.ID].NextCredentialIndex["kimi"] != 5 {
		t.Fatal("failed mutation leaked into committed credential index")
	}
	if len(s.data.Invites[invite.ID].UsedBy) != 0 {
		t.Fatal("failed mutation consumed invite usage")
	}
	oldIndex := s.data.Channels[ch.ID].NextCredentialIndex
	s.mu.RUnlock()

	// Returned values are clones: mutating them must not corrupt the store.
	keys[0].BlockedModels[0] = "mutated-from-outside"
	if fresh := s.Keys(bob); fresh[0].BlockedModels[0] != "gpt-4" {
		t.Fatalf("returned key aliases committed state: %+v", fresh[0])
	}

	// The failed registration did not consume the invite.
	s.persist = atomicWrite
	if _, err := s.Register("carol", "password-carol", invite.Code); err != nil {
		t.Fatalf("failed save consumed invite: %v", err)
	}
	invites, err := s.Invites(admin)
	if err != nil {
		t.Fatal(err)
	}
	retried := false
	for _, inv := range invites {
		if inv.ID != invite.ID {
			continue
		}
		if len(inv.UsedBy) != 1 || inv.UsedBy[0] != "carol" {
			t.Fatalf("invite state wrong after successful retry: %+v", inv)
		}
		retried = true
	}
	if !retried {
		t.Fatalf("invite %s missing after successful retry: %+v", invite.ID, invites)
	}

	// A successful publish installs a fresh structure graph: references to the
	// previous committed generation detach instead of aliasing the new one.
	if _, err := s.AllocateCredentialIndex(ch.ID, "kimi"); err != nil {
		t.Fatal(err)
	}
	if oldIndex["kimi"] != 5 {
		t.Fatalf("old generation aliased new committed state: %d", oldIndex["kimi"])
	}
	s.mu.RLock()
	if s.data.Channels[ch.ID].NextCredentialIndex["kimi"] != 6 {
		t.Fatalf("new generation not published: %d", s.data.Channels[ch.ID].NextCredentialIndex["kimi"])
	}
	s.mu.RUnlock()
}

// TestKeyHashIndexInvalidatesImmediately covers the hash->ID index lifecycle:
// O(1) lookups behave like the old scan, disable/revoke and owner state take
// effect immediately on publish, and Open rebuilds the index from disk.
func TestKeyHashIndexInvalidatesImmediately(t *testing.T) {
	s, admin, bob := testAccounts(t)
	keyA, secretA, err := s.CreateKey(bob, "primary")
	if err != nil {
		t.Fatal(err)
	}
	if user, got, ok := s.AuthenticateKey(secretA); !ok || got.ID != keyA.ID || user.Username != bob.Username {
		t.Fatalf("key A did not authenticate: ok=%v", ok)
	}
	if _, _, ok := s.AuthenticateKey("sk-hub-definitely-unknown"); ok {
		t.Fatal("unknown key authenticated")
	}

	disabled := true
	if err := s.UpdateKey(bob, keyA.ID, nil, &disabled, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretA); ok {
		t.Fatal("disabled key authenticated after publish")
	}
	enabled := false
	if err := s.UpdateKey(bob, keyA.ID, nil, &enabled, nil, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretA); !ok {
		t.Fatal("re-enabled key failed to authenticate")
	}
	if err := s.UpdateKey(bob, keyA.ID, nil, nil, nil, nil, true); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretA); ok {
		t.Fatal("revoked key authenticated")
	}

	// A key created after revocation gets an index entry on publish.
	_, secretB, err := s.CreateKey(bob, "secondary")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretB); !ok {
		t.Fatal("new key missing from index")
	}

	// Owner state is checked from the record, not the index.
	if err := s.UpdateUser(admin, bob.Username, &disabled, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretB); ok {
		t.Fatal("key of disabled owner authenticated")
	}
	if err := s.UpdateUser(admin, bob.Username, &enabled, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.AuthenticateKey(secretB); !ok {
		t.Fatal("key of re-enabled owner failed to authenticate")
	}

	s.mu.RLock()
	if len(s.keyIndex) != len(s.data.Keys) {
		t.Fatalf("index size %d != key count %d", len(s.keyIndex), len(s.data.Keys))
	}
	s.mu.RUnlock()

	// Open rebuilds the index from the persisted file.
	reopened, err := Open(s.dir, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := reopened.AuthenticateKey(secretB); !ok {
		t.Fatal("reopened store lost key B from index")
	}
	if _, _, ok := reopened.AuthenticateKey(secretA); ok {
		t.Fatal("reopened store authenticated revoked key")
	}
	reopened.mu.RLock()
	if len(reopened.keyIndex) != len(reopened.data.Keys) {
		t.Fatalf("reopened index size %d != key count %d", len(reopened.keyIndex), len(reopened.data.Keys))
	}
	reopened.mu.RUnlock()
}
