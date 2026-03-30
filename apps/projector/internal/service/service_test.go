package service

import (
	"testing"
	"time"

	domain "votingplatform/projector/internal/domain"
)

func TestStateApplyVoteAndRecompute(t *testing.T) {
	state := NewState()
	state.UpsertVoting(domain.Voting{VotingID: "v1", Candidates: []domain.Candidate{{CandidateID: "c1", Name: "Alice"}, {CandidateID: "c2", Name: "Bob"}}})

	vote1 := domain.VoteRawEvent{VoteID: "vote-1", VotingID: "v1", CandidateID: "c1", IP: "ip-1", OccurredAt: time.Now().UTC()}
	snap, ok := ApplyVote(state, vote1, 2, 41)
	if !ok || snap.TotalVotes != 1 || snap.ByCandidate["c1"] != 1 {
		t.Fatalf("expected first vote to be applied, got snap=%+v ok=%v", snap, ok)
	}
	if got := snap.ReplayMetadata.InitialOffsetsByPartition["2"]; got != 41 {
		t.Fatalf("expected initial partition offset to be tracked, got %d", got)
	}

	retro := domain.PolicyControlEvent{VotingID: "v1", TargetValue: "ip-1", Action: "ACTIVATE", EffectiveMode: "RETROACTIVE"}
	if !state.ApplyPolicy(retro) {
		t.Fatalf("expected retroactive policy to request recompute")
	}

	voting, base, blocked, ok := state.RecomputeInputs("v1")
	if !ok {
		t.Fatalf("expected recompute inputs to exist")
	}
	snap = RecomputeSnapshot(voting, base, blocked, []domain.VoteRawEvent{vote1}, retro.PolicyEventID)
	if snap.TotalVotes != 0 {
		t.Fatalf("expected recompute to remove blocked vote, got snap=%+v", snap)
	}
}
