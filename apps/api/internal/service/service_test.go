package service

import (
	"testing"
	"time"
)

func TestPolicyState(t *testing.T) {
	state := NewPolicyState()
	votingID := "v1"
	ip := "198.51.100.10"

	if state.IsBlocked(votingID, ip) {
		t.Fatalf("expected ip to start unblocked")
	}

	state.SetBlocked(votingID, ip, true)
	if !state.IsBlocked(votingID, ip) {
		t.Fatalf("expected ip to be blocked")
	}

	state.SetBlocked(votingID, ip, false)
	if state.IsBlocked(votingID, ip) {
		t.Fatalf("expected ip to be unblocked after deactivation")
	}
}

func TestPolicyStateTemporaryBlock(t *testing.T) {
	state := NewPolicyState()
	votingID := "v1"
	ip := "198.51.100.11"

	state.SetBlockedUntil(votingID, ip, time.Now().UTC().Add(time.Hour))
	if !state.IsBlocked(votingID, ip) {
		t.Fatalf("expected ip to be temporarily blocked")
	}

	state.SetBlockedUntil(votingID, ip, time.Now().UTC().Add(-time.Minute))
	if !state.IsBlocked(votingID, ip) {
		t.Fatalf("expected longer existing block not to be shortened")
	}

	otherIP := "198.51.100.12"
	state.SetBlockedUntil(votingID, otherIP, time.Now().UTC().Add(-time.Second))
	if state.IsBlocked(votingID, otherIP) {
		t.Fatalf("expected expired temporary block to be ignored")
	}
}

func TestVoteStatusStore(t *testing.T) {
	store := NewVoteStatusStore()
	now := time.Now().UTC()
	store.Set("vote-1", VoteStatusEntry{VotingID: "v1", Status: VoteStatusPending, UpdatedAt: now})

	entry, ok := store.Get("vote-1")
	if !ok || entry.Status != VoteStatusPending {
		t.Fatalf("expected pending vote status, got %+v exists=%v", entry, ok)
	}

	updated := store.Update("vote-1", func(entry VoteStatusEntry) VoteStatusEntry {
		entry.Status = VoteStatusWritten
		entry.UpdatedAt = now.Add(time.Second)
		return entry
	})
	if !updated {
		t.Fatalf("expected update to succeed")
	}

	entry, ok = store.Get("vote-1")
	if !ok || entry.Status != VoteStatusWritten {
		t.Fatalf("expected written vote status, got %+v exists=%v", entry, ok)
	}

	store.Set("vote-2", VoteStatusEntry{VotingID: "v2", Status: VoteStatusFailed, UpdatedAt: now.Add(-2 * time.Minute)})
	store.CleanupOlderThan(now.Add(-time.Minute))
	if _, ok := store.Get("vote-2"); ok {
		t.Fatalf("expected expired entry to be cleaned up")
	}
}

func TestMemoryAntiAbuseStoreMarksChallengeUsed(t *testing.T) {
	store := NewMemoryAntiAbuseStore()
	expiresAt := time.Now().UTC().Add(time.Minute)
	marked, err := store.MarkChallengeUsed("challenge-1", expiresAt)
	if err != nil {
		t.Fatalf("mark first challenge use: %v", err)
	}
	if !marked {
		t.Fatalf("expected first mark to succeed")
	}
	marked, err = store.MarkChallengeUsed("challenge-1", expiresAt)
	if err != nil {
		t.Fatalf("mark reused challenge: %v", err)
	}
	if marked {
		t.Fatalf("expected reused challenge to be rejected")
	}
}

func TestMemoryAntiAbuseStoreCountsVotesByIP(t *testing.T) {
	store := NewMemoryAntiAbuseStore()
	now := time.Now().UTC()
	window := 30 * time.Second
	if err := store.RecordVoteAccepted("v1", "198.51.100.10", "s1", now.Add(-20*time.Second), window); err != nil {
		t.Fatalf("record first vote: %v", err)
	}
	if err := store.RecordVoteAccepted("v1", "198.51.100.10", "s2", now.Add(-10*time.Second), window); err != nil {
		t.Fatalf("record second vote: %v", err)
	}
	if err := store.RecordVoteAccepted("v1", "198.51.100.11", "s3", now.Add(-5*time.Second), window); err != nil {
		t.Fatalf("record other ip vote: %v", err)
	}
	count, err := store.CountRecentVotesByIP("v1", "198.51.100.10", now.Add(-window))
	if err != nil {
		t.Fatalf("count votes by ip: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 votes for ip, got %d", count)
	}
	count, err = store.CountRecentVotesByIP("v1", "198.51.100.11", now.Add(-window))
	if err != nil {
		t.Fatalf("count other ip votes: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 vote for other ip, got %d", count)
	}
}

func TestMemoryAntiAbuseStoreTracksSessionActivitySeparately(t *testing.T) {
	store := NewMemoryAntiAbuseStore()
	now := time.Now().UTC()
	window := time.Minute
	if err := store.RecordChallengeIssued("v1", "198.51.100.10", "s1", now.Add(-20*time.Second), window); err != nil {
		t.Fatalf("record session s1: %v", err)
	}
	if err := store.RecordChallengeIssued("v1", "198.51.100.10", "s2", now.Add(-10*time.Second), window); err != nil {
		t.Fatalf("record session s2: %v", err)
	}
	count, err := store.CountRecentSessionActivity("v1", "198.51.100.10", "s1", now.Add(-window))
	if err != nil {
		t.Fatalf("count session activity: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 event for session s1, got %d", count)
	}
	count, err = store.CountRecentChallengesByIP("v1", "198.51.100.10", now.Add(-window))
	if err != nil {
		t.Fatalf("count challenge activity: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 challenge events for ip, got %d", count)
	}
}
