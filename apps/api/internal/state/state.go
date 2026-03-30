package state

import (
	"sync"
	"time"
)

type VoteStatus string

const (
	VoteStatusPending VoteStatus = "PENDING"
	VoteStatusWritten VoteStatus = "WRITTEN"
	VoteStatusFailed  VoteStatus = "FAILED"
)

type VoteStatusEntry struct {
	VotingID  string
	Status    VoteStatus
	UpdatedAt time.Time
}

type VoteStatusStore struct {
	mu      sync.RWMutex
	entries map[string]VoteStatusEntry
}

func NewVoteStatusStore() *VoteStatusStore {
	return &VoteStatusStore{entries: make(map[string]VoteStatusEntry)}
}

func (s *VoteStatusStore) Set(voteID string, entry VoteStatusEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[voteID] = entry
}

func (s *VoteStatusStore) Get(voteID string) (VoteStatusEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[voteID]
	return entry, ok
}

func (s *VoteStatusStore) Delete(voteID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, voteID)
}

func (s *VoteStatusStore) Update(voteID string, fn func(VoteStatusEntry) VoteStatusEntry) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[voteID]
	if !ok {
		return false
	}
	s.entries[voteID] = fn(entry)
	return true
}

func (s *VoteStatusStore) CleanupOlderThan(cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for voteID, entry := range s.entries {
		if entry.UpdatedAt.Before(cutoff) {
			delete(s.entries, voteID)
		}
	}
}

type PolicyState struct {
	mu      sync.Mutex
	blocked map[string]map[string]time.Time
}

func NewPolicyState() *PolicyState {
	return &PolicyState{blocked: make(map[string]map[string]time.Time)}
}

func (s *PolicyState) IsBlocked(votingID, ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if byVoting, ok := s.blocked[votingID]; ok {
		until, blocked := byVoting[ip]
		if !blocked {
			return false
		}
		if until.IsZero() || time.Now().UTC().Before(until) {
			return true
		}
		delete(byVoting, ip)
		if len(byVoting) == 0 {
			delete(s.blocked, votingID)
		}
	}
	return false
}

func (s *PolicyState) SetBlocked(votingID, ip string, active bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blocked[votingID]; !ok {
		s.blocked[votingID] = make(map[string]time.Time)
	}
	if active {
		s.blocked[votingID][ip] = time.Time{}
		return
	}
	delete(s.blocked[votingID], ip)
	if len(s.blocked[votingID]) == 0 {
		delete(s.blocked, votingID)
	}
}

func (s *PolicyState) SetBlockedUntil(votingID, ip string, until time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blocked[votingID]; !ok {
		s.blocked[votingID] = make(map[string]time.Time)
	}
	current, exists := s.blocked[votingID][ip]
	if exists {
		if current.IsZero() || current.After(until) {
			return
		}
	}
	s.blocked[votingID][ip] = until.UTC()
}

type UsedChallengeStore struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func NewUsedChallengeStore() *UsedChallengeStore {
	return &UsedChallengeStore{entries: make(map[string]time.Time)}
}

func (s *UsedChallengeStore) SetUsed(challengeID string, expiresAt time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, expiry := range s.entries {
		if !expiry.IsZero() && !expiry.After(now) {
			delete(s.entries, id)
		}
	}
	if expiry, exists := s.entries[challengeID]; exists && (expiry.IsZero() || expiry.After(now)) {
		return false
	}
	s.entries[challengeID] = expiresAt.UTC()
	return true
}

func (s *UsedChallengeStore) IsUsed(challengeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiresAt, exists := s.entries[challengeID]
	if !exists {
		return false
	}
	if !expiresAt.IsZero() && !expiresAt.After(time.Now().UTC()) {
		delete(s.entries, challengeID)
		return false
	}
	return true
}

func (s *UsedChallengeStore) CleanupExpired(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for challengeID, expiresAt := range s.entries {
		if !expiresAt.IsZero() && !expiresAt.After(now.UTC()) {
			delete(s.entries, challengeID)
		}
	}
}

type IPActivityStore struct {
	mu      sync.Mutex
	entries map[string][]time.Time
}

func NewIPActivityStore() *IPActivityStore {
	return &IPActivityStore{entries: make(map[string][]time.Time)}
}

func (s *IPActivityStore) Add(ip string, at time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := at.UTC()
	existing := s.entries[ip]
	existing = append(existing, now)
	s.entries[ip] = existing
}

func (s *IPActivityStore) CountSince(ip string, cutoff time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := s.entries[ip]
	if len(entries) == 0 {
		return 0
	}
	cutoff = cutoff.UTC()
	kept := entries[:0]
	count := 0
	for _, ts := range entries {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
			count++
		}
	}
	if len(kept) == 0 {
		delete(s.entries, ip)
	} else {
		s.entries[ip] = kept
	}
	return count
}

func (s *IPActivityStore) CleanupOlderThan(cutoff time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff = cutoff.UTC()
	for ip, entries := range s.entries {
		kept := entries[:0]
		for _, ts := range entries {
			if !ts.Before(cutoff) {
				kept = append(kept, ts)
			}
		}
		if len(kept) == 0 {
			delete(s.entries, ip)
		} else {
			s.entries[ip] = kept
		}
	}
}
