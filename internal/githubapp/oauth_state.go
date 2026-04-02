package githubapp

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

type oauthStateStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func newOAuthStateStore() *oauthStateStore {
	s := &oauthStateStore{entries: make(map[string]time.Time)}
	go s.cleanupLoop()
	return s
}

func (s *oauthStateStore) cleanupLoop() {
	t := time.NewTicker(5 * time.Minute)
	for range t.C {
		s.prune(15 * time.Minute)
	}
}

func (s *oauthStateStore) prune(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, ts := range s.entries {
		if ts.Before(cutoff) {
			delete(s.entries, k)
		}
	}
}

func (s *oauthStateStore) issue() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	state := hex.EncodeToString(b[:])
	s.mu.Lock()
	s.entries[state] = time.Now()
	s.mu.Unlock()
	return state, nil
}

func (s *oauthStateStore) consume(state string) bool {
	if state == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[state]; !ok {
		return false
	}
	delete(s.entries, state)
	return true
}
