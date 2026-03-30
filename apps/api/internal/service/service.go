package service

import statepkg "votingplatform/api/internal/state"

type VoteStatus = statepkg.VoteStatus

type VoteStatusEntry = statepkg.VoteStatusEntry

type VoteStatusStore = statepkg.VoteStatusStore

type PolicyState = statepkg.PolicyState
type UsedChallengeStore = statepkg.UsedChallengeStore
type IPActivityStore = statepkg.IPActivityStore

const (
	VoteStatusPending = statepkg.VoteStatusPending
	VoteStatusWritten = statepkg.VoteStatusWritten
	VoteStatusFailed  = statepkg.VoteStatusFailed
)

func NewVoteStatusStore() *VoteStatusStore {
	return statepkg.NewVoteStatusStore()
}

func NewPolicyState() *PolicyState {
	return statepkg.NewPolicyState()
}

func NewUsedChallengeStore() *UsedChallengeStore {
	return statepkg.NewUsedChallengeStore()
}

func NewIPActivityStore() *IPActivityStore {
	return statepkg.NewIPActivityStore()
}
