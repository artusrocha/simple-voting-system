package state

import (
	"strconv"
	"sync"

	domain "votingplatform/projector/internal/domain"
)

type State struct {
	mu sync.RWMutex

	votings      map[string]domain.Voting
	blocked      map[string]map[string]bool
	retroBlocked map[string]map[string]bool
	snapshots    map[string]domain.ResultsSnapshotEvent
}

func New() *State {
	return &State{
		votings:      make(map[string]domain.Voting),
		blocked:      make(map[string]map[string]bool),
		retroBlocked: make(map[string]map[string]bool),
		snapshots:    make(map[string]domain.ResultsSnapshotEvent),
	}
}

func (s *State) UpsertVoting(v domain.Voting) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.votings[v.VotingID] = v
}

func (s *State) ApplyPolicy(evt domain.PolicyControlEvent) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.blocked[evt.VotingID]; !ok {
		s.blocked[evt.VotingID] = make(map[string]bool)
	}
	if _, ok := s.retroBlocked[evt.VotingID]; !ok {
		s.retroBlocked[evt.VotingID] = make(map[string]bool)
	}
	active := evt.Action == "ACTIVATE"
	if active {
		s.blocked[evt.VotingID][evt.TargetValue] = true
	} else {
		delete(s.blocked[evt.VotingID], evt.TargetValue)
	}
	if evt.EffectiveMode == "RETROACTIVE" {
		if active {
			s.retroBlocked[evt.VotingID][evt.TargetValue] = true
		} else {
			delete(s.retroBlocked[evt.VotingID], evt.TargetValue)
		}
		return true
	}
	return false
}

func (s *State) ApplyVoteInputs(evt domain.VoteRawEvent) (domain.Voting, domain.ResultsSnapshotEvent, bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, exists := s.votings[evt.VotingID]
	if !exists || s.blocked[evt.VotingID][evt.IP] {
		return domain.Voting{}, domain.ResultsSnapshotEvent{}, false, false
	}
	snap, ok := s.snapshots[evt.VotingID]
	return v, snap, ok, true
}

func (s *State) RecomputeInputs(votingID string) (domain.Voting, domain.ResultsSnapshotEvent, map[string]bool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, exists := s.votings[votingID]
	if !exists {
		return domain.Voting{}, domain.ResultsSnapshotEvent{}, nil, false
	}
	snap := s.snapshots[votingID]
	blocked := make(map[string]bool, len(s.retroBlocked[votingID]))
	for ip, active := range s.retroBlocked[votingID] {
		blocked[ip] = active
	}
	return v, snap, blocked, true
}

func (s *State) SaveSnapshot(votingID string, snap domain.ResultsSnapshotEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[votingID] = snap
}

func (s *State) Snapshot(votingID string) (domain.ResultsSnapshotEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[votingID]
	return snap, ok
}

func (s *State) VotingIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.votings))
	for votingID := range s.votings {
		ids = append(ids, votingID)
	}
	return ids
}

func (s *State) ShouldSkipVote(votingID string, partition int32, offset int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[votingID]
	if !ok {
		return false
	}
	last, ok := snap.ReplayMetadata.LastProcessedOffsetsByPartition[strconv.FormatInt(int64(partition), 10)]
	return ok && offset <= last
}
