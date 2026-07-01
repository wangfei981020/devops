// Copyright (c) 2025 kk
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package handlers

import (
	"sync"
	"time"
)

const oidcStateTTL = 10 * time.Minute

// oidcStateStore holds OAuth state server-side when the oidc_state cookie is not
// sent back after the cross-site redirect from the IdP (common with ppu-sso).
type oidcStateStore struct {
	mu     sync.Mutex
	states map[string]time.Time
}

var globalOIDCStateStore = &oidcStateStore{
	states: make(map[string]time.Time),
}

func (s *oidcStateStore) register(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked()
	s.states[state] = time.Now().Add(oidcStateTTL)
}

func (s *oidcStateStore) valid(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked()
	exp, ok := s.states[state]
	return ok && time.Now().Before(exp)
}

func (s *oidcStateStore) consume(state string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.states[state]
	if !ok || time.Now().After(exp) {
		delete(s.states, state)
		return false
	}
	delete(s.states, state)
	return true
}

func (s *oidcStateStore) purgeExpiredLocked() {
	now := time.Now()
	for state, exp := range s.states {
		if now.After(exp) {
			delete(s.states, state)
		}
	}
}
