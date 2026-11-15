package plugin

import (
	"fmt"
	"sync"
	"time"

	"github.com/nianyuechan/otonashi/go-yunzai/pkg/plugin/sdk"
)

type sessionKey struct {
	pluginID    string
	accountID   int64
	userID      int64
	groupID     int64
	messageType string
}

type pendingSession struct {
	handler sdk.HandlerFunc
	expires time.Time
	timer   *time.Timer
}

type sessionStore struct {
	mu      sync.Mutex
	entries map[sessionKey]*pendingSession
	now     func() time.Time
	closed  bool
}

type scopedSession struct {
	store *sessionStore
	key   sessionKey
}

func newSessionStore(now func() time.Time) *sessionStore {
	return &sessionStore{entries: make(map[sessionKey]*pendingSession), now: now}
}

func (s *sessionStore) scope(pluginID string, evt *sdk.Event) scopedSession {
	return scopedSession{store: s, key: sessionKey{
		pluginID: pluginID, accountID: evt.SelfID, userID: evt.UserID,
		groupID: evt.GroupID, messageType: evt.MessageType,
	}}
}

func (s scopedSession) Set(timeout time.Duration, handler sdk.HandlerFunc) error {
	if timeout <= 0 || handler == nil {
		return fmt.Errorf("session requires a positive timeout and handler")
	}
	if s.key.userID <= 0 || (s.key.messageType != "private" && s.key.messageType != "group") || (s.key.messageType == "group" && s.key.groupID <= 0) {
		return fmt.Errorf("session requires a private or group message with a user id")
	}
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if s.store.closed {
		return sdk.ErrNotRunning
	}
	if old := s.store.entries[s.key]; old != nil {
		old.timer.Stop()
	}
	pending := &pendingSession{handler: handler, expires: s.store.now().Add(timeout)}
	pending.timer = time.AfterFunc(timeout, func() {
		s.store.mu.Lock()
		defer s.store.mu.Unlock()
		if s.store.entries[s.key] == pending {
			delete(s.store.entries, s.key)
		}
	})
	s.store.entries[s.key] = pending
	return nil
}

func (s scopedSession) Clear() {
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	if old := s.store.entries[s.key]; old != nil {
		old.timer.Stop()
		delete(s.store.entries, s.key)
	}
}

func (s *sessionStore) take(key sessionKey) sdk.HandlerFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.entries[key]
	if pending == nil {
		return nil
	}
	delete(s.entries, key)
	pending.timer.Stop()
	if s.closed || !s.now().Before(pending.expires) {
		return nil
	}
	return pending.handler
}

func (s *sessionStore) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for key, pending := range s.entries {
		pending.timer.Stop()
		delete(s.entries, key)
	}
}
