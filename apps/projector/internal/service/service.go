package service

import (
	"strconv"
	"time"

	domain "votingplatform/projector/internal/domain"
	statepkg "votingplatform/projector/internal/state"
)

type State = statepkg.State

func NewState() *State {
	return statepkg.New()
}

func ApplyVote(st *State, evt domain.VoteRawEvent, partition int32, offset int64) (domain.ResultsSnapshotEvent, bool) {
	voting, snap, hasSnapshot, ok := func() (domain.Voting, domain.ResultsSnapshotEvent, bool, bool) {
		v, existing, has, ok := st.ApplyVoteInputs(evt)
		return v, existing, has, ok
	}()
	if !ok || !domain.HasCandidate(voting.Candidates, evt.CandidateID) {
		return domain.ResultsSnapshotEvent{}, false
	}
	if !hasSnapshot {
		snap = domain.NewEmptySnapshot(evt.VotingID, voting.Candidates)
	}
	snap.TotalVotes++
	snap.ByCandidate[evt.CandidateID]++
	hourKey := evt.OccurredAt.UTC().Truncate(time.Hour).Format(time.RFC3339)
	snap.ByHour[hourKey]++
	snap.Checkpoint = evt.VoteID
	snap.UpdatedAt = time.Now().UTC()
	TrackReplayOffset(&snap, partition, offset)
	domain.RecomputePercentages(&snap)
	st.SaveSnapshot(evt.VotingID, snap)
	return snap, true
}

func RecomputeSnapshot(voting domain.Voting, base domain.ResultsSnapshotEvent, blocked map[string]bool, votes []domain.VoteRawEvent, checkpoint string) domain.ResultsSnapshotEvent {
	snap := domain.NewEmptySnapshot(voting.VotingID, voting.Candidates)
	snap.ReplayMetadata = cloneReplayMetadata(base.ReplayMetadata)
	for _, vote := range votes {
		if blocked[vote.IP] || !domain.HasCandidate(voting.Candidates, vote.CandidateID) {
			continue
		}
		snap.TotalVotes++
		snap.ByCandidate[vote.CandidateID]++
		hourKey := vote.OccurredAt.UTC().Truncate(time.Hour).Format(time.RFC3339)
		snap.ByHour[hourKey]++
	}
	domain.RecomputePercentages(&snap)
	snap.Checkpoint = checkpoint
	snap.UpdatedAt = time.Now().UTC()
	return snap
}

func TrackReplayOffset(snap *domain.ResultsSnapshotEvent, partition int32, offset int64) {
	key := partitionKey(partition)
	if snap.ReplayMetadata.InitialOffsetsByPartition == nil {
		snap.ReplayMetadata.InitialOffsetsByPartition = map[string]int64{}
	}
	if snap.ReplayMetadata.LastProcessedOffsetsByPartition == nil {
		snap.ReplayMetadata.LastProcessedOffsetsByPartition = map[string]int64{}
	}
	if _, exists := snap.ReplayMetadata.InitialOffsetsByPartition[key]; !exists {
		snap.ReplayMetadata.InitialOffsetsByPartition[key] = offset
	}
	if last, exists := snap.ReplayMetadata.LastProcessedOffsetsByPartition[key]; !exists || offset > last {
		snap.ReplayMetadata.LastProcessedOffsetsByPartition[key] = offset
	}
}

func partitionKey(partition int32) string {
	return strconv.FormatInt(int64(partition), 10)
}

func cloneReplayMetadata(meta domain.ReplayMetadata) domain.ReplayMetadata {
	cloned := domain.ReplayMetadata{
		InitialOffsetsByPartition:       map[string]int64{},
		LastProcessedOffsetsByPartition: map[string]int64{},
	}
	for key, value := range meta.InitialOffsetsByPartition {
		cloned.InitialOffsetsByPartition[key] = value
	}
	for key, value := range meta.LastProcessedOffsetsByPartition {
		cloned.LastProcessedOffsetsByPartition[key] = value
	}
	return cloned
}
