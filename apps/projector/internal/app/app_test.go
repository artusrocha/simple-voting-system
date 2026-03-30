package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	configpkg "votingplatform/projector/internal/config"
	domain "votingplatform/projector/internal/domain"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

func TestNewEmptySnapshot(t *testing.T) {
	candidates := []domain.Candidate{{CandidateID: "c1", Name: "Alice"}, {CandidateID: "c2", Name: "Bob"}}
	snap := domain.NewEmptySnapshot("v1", candidates)

	if snap.VotingID != "v1" {
		t.Fatalf("expected voting id v1, got %q", snap.VotingID)
	}
	if snap.TotalVotes != 0 {
		t.Fatalf("expected 0 votes, got %d", snap.TotalVotes)
	}
	if snap.ByCandidate["c1"] != 0 || snap.ByCandidate["c2"] != 0 {
		t.Fatalf("expected zeroed candidate counters, got %+v", snap.ByCandidate)
	}
	if snap.PercentageByCandidate["c1"] != 0 || snap.PercentageByCandidate["c2"] != 0 {
		t.Fatalf("expected zeroed percentages, got %+v", snap.PercentageByCandidate)
	}
}

func TestRecomputePercentages(t *testing.T) {
	snap := domain.ResultsSnapshotEvent{
		ByCandidate:           map[string]int64{"c1": 1, "c2": 3},
		PercentageByCandidate: map[string]float64{"c1": 0, "c2": 0},
		TotalVotes:            4,
	}

	domain.RecomputePercentages(&snap)

	if snap.PercentageByCandidate["c1"] != 25 {
		t.Fatalf("expected c1=25%%, got %v", snap.PercentageByCandidate["c1"])
	}
	if snap.PercentageByCandidate["c2"] != 75 {
		t.Fatalf("expected c2=75%%, got %v", snap.PercentageByCandidate["c2"])
	}
}

func TestRecomputePercentagesWithZeroVotes(t *testing.T) {
	snap := domain.ResultsSnapshotEvent{
		ByCandidate:           map[string]int64{"c1": 0, "c2": 0},
		PercentageByCandidate: map[string]float64{"c1": 99, "c2": 42},
		TotalVotes:            0,
	}

	domain.RecomputePercentages(&snap)

	if snap.PercentageByCandidate["c1"] != 0 || snap.PercentageByCandidate["c2"] != 0 {
		t.Fatalf("expected all percentages to reset to 0, got %+v", snap.PercentageByCandidate)
	}
}

func TestHasCandidate(t *testing.T) {
	candidates := []domain.Candidate{{CandidateID: "c1", Name: "Alice"}}

	if !domain.HasCandidate(candidates, "c1") {
		t.Fatalf("expected candidate c1 to exist")
	}
	if domain.HasCandidate(candidates, "c9") {
		t.Fatalf("expected candidate c9 not to exist")
	}
}

func TestIsValidStatus(t *testing.T) {
	if !domain.IsValidStatus("OPEN") {
		t.Fatalf("expected OPEN to be valid")
	}
	if domain.IsValidStatus("BROKEN") {
		t.Fatalf("expected BROKEN to be invalid")
	}
}

func TestVotingCatalogEventPreservesArgon2FormulaFields(t *testing.T) {
	raw := []byte(`{
		"eventId":"evt-1",
		"occurredAt":"2026-03-28T12:00:00Z",
		"voting":{
			"votingId":"v1",
			"name":"Test",
			"status":"OPEN",
			"candidates":[{"candidateId":"c1","name":"Alice"}],
			"antiAbuse":{
				"honeypotEnabled":true,
				"slideVoteMode":"off",
				"interactionTelemetryEnabled":false,
				"pow":{
					"enabled":true,
					"algorithm":"argon2id",
					"ttlSeconds":90,
					"baseDifficultyBits":8,
					"maxDifficultyBits":10,
					"adaptiveWindowSeconds":60,
					"baseMemoryKiB":4096,
					"memoryGrowthFactor":1.2,
					"difficultyStepEvery":4,
					"memoryKiB":4096,
					"timeCost":1,
					"parallelism":1,
					"hashLength":16
				}
			},
			"createdAt":"2026-03-28T12:00:00Z"
		}
	}`)

	var evt votingCatalogEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if evt.Voting.AntiAbuse.Pow.BaseMemoryKiB != 4096 || evt.Voting.AntiAbuse.Pow.MemoryGrowthFactor != 1.2 || evt.Voting.AntiAbuse.Pow.DifficultyStepEvery != 4 {
		t.Fatalf("formula fields were not preserved: %+v", evt.Voting.AntiAbuse.Pow)
	}
}

func TestVoteRawEventPreservesTelemetryFields(t *testing.T) {
	raw := []byte(`{
		"voteId":"vote-1",
		"votingId":"v1",
		"candidateId":"c1",
		"occurredAt":"2026-03-28T12:00:05Z",
		"ip":"198.51.100.10",
		"pow":{
			"challengeId":"challenge-1",
			"algorithm":"argon2id",
			"difficultyBits":9,
			"params":{"difficultyBits":9,"memoryKiB":4096,"timeCost":1,"parallelism":1,"hashLength":16},
			"validated":true,
			"solveDurationMs":1200,
			"issueToSubmitMs":4300
		},
		"interaction":{
			"moveEvents":12,
			"maxProgress":0.97,
			"completed":true,
			"mode":"full"
		},
		"client":{
			"userAgent":"test-agent",
			"platform":"Linux x86_64",
			"viewportWidth":1440,
			"viewportHeight":900
		},
		"requestContext":{
			"ip":"198.51.100.10",
			"sessionId":"session-1",
			"userAgent":"test-agent",
			"forwardedFor":"198.51.100.10, 10.0.0.1",
			"receivedAt":"2026-03-28T12:00:05Z",
			"confirm":true
		},
		"antiAbuseRuntime":{
			"storeBackend":"valkey",
			"reuseCheckStatus":"failed_open",
			"challengeIssueRecordStatus":"failed",
			"voteActivityRecordStatus":"ok",
			"sessionActivityRecordStatus":"failed",
			"errors":["challenge_reuse_check_failed","challenge_issue_record_failed"]
		}
	}`)

	var evt domain.VoteRawEvent
	if err := json.Unmarshal(raw, &evt); err != nil {
		t.Fatalf("unmarshal vote raw event: %v", err)
	}
	if evt.Pow == nil || evt.Pow.IssueToSubmitMs != 4300 || evt.Pow.Params.MemoryKiB != 4096 {
		t.Fatalf("unexpected pow payload: %+v", evt.Pow)
	}
	if evt.Interaction == nil || evt.Interaction.MoveEvents != 12 || !evt.Interaction.Completed {
		t.Fatalf("unexpected interaction payload: %+v", evt.Interaction)
	}
	if evt.Client == nil || evt.Client.UserAgent != "test-agent" {
		t.Fatalf("unexpected client payload: %+v", evt.Client)
	}
	if evt.RequestContext == nil || evt.RequestContext.SessionID != "session-1" || !evt.RequestContext.Confirm {
		t.Fatalf("unexpected request context: %+v", evt.RequestContext)
	}
	if evt.AntiAbuseRuntime == nil || evt.AntiAbuseRuntime.ReuseCheckStatus != "failed_open" || len(evt.AntiAbuseRuntime.Errors) != 2 {
		t.Fatalf("unexpected anti-abuse runtime payload: %+v", evt.AntiAbuseRuntime)
	}
}

func TestShouldRecycleConsumer(t *testing.T) {
	tests := []struct {
		name              string
		err               error
		consecutiveErrors int
		want              bool
	}{
		{name: "too few errors", err: errors.New("brokers are down"), consecutiveErrors: 2, want: false},
		{name: "broker down", err: errors.New("2/2 brokers are down"), consecutiveErrors: 3, want: true},
		{name: "dns resolution", err: errors.New("Failed to resolve 'kafka:9092'"), consecutiveErrors: 4, want: true},
		{name: "unrelated error", err: errors.New("temporary decode issue"), consecutiveErrors: 5, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldRecycleConsumer(tc.err, tc.consecutiveErrors); got != tc.want {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestFlushPendingSnapshotsPublishesLatestPerVoting(t *testing.T) {
	app := &application{
		cfg:         configpkg.Config{TopicResultsSnapshot: "voting-results-snapshot"},
		logger:      slog.Default(),
		pending:     make(map[string]resultsSnapshotEvent),
		dirty:       make(map[string]struct{}),
		metricIndex: make(map[string]snapshotMetricIndex),
	}

	originalPublish := publishJSON
	defer func() { publishJSON = originalPublish }()

	var published []resultsSnapshotEvent
	publishJSON = func(_ *kafka.Producer, _ string, _ []byte, payload any) error {
		snap, ok := payload.(resultsSnapshotEvent)
		if !ok {
			t.Fatalf("unexpected payload type %T", payload)
		}
		published = append(published, snap)
		return nil
	}

	app.enqueueSnapshotForPublish(resultsSnapshotEvent{VotingID: "v1", TotalVotes: 1})
	app.enqueueSnapshotForPublish(resultsSnapshotEvent{VotingID: "v1", TotalVotes: 2})
	app.enqueueSnapshotForPublish(resultsSnapshotEvent{VotingID: "v2", TotalVotes: 3})
	app.flushPendingSnapshots()

	if len(published) != 2 {
		t.Fatalf("expected 2 published snapshots, got %d", len(published))
	}
	if published[0].VotingID == "v1" && published[0].TotalVotes != 2 {
		t.Fatalf("expected latest v1 snapshot to win, got %+v", published[0])
	}
	if published[1].VotingID == "v1" && published[1].TotalVotes != 2 {
		t.Fatalf("expected latest v1 snapshot to win, got %+v", published[1])
	}
	if len(app.dirty) != 0 {
		t.Fatalf("expected dirty set to be empty after flush")
	}
}

func TestFlushPendingSnapshotsRequeuesOnPublishFailure(t *testing.T) {
	app := &application{
		cfg:         configpkg.Config{TopicResultsSnapshot: "voting-results-snapshot"},
		logger:      slog.Default(),
		pending:     make(map[string]resultsSnapshotEvent),
		dirty:       make(map[string]struct{}),
		metricIndex: make(map[string]snapshotMetricIndex),
	}

	originalPublish := publishJSON
	defer func() { publishJSON = originalPublish }()
	publishJSON = func(_ *kafka.Producer, _ string, _ []byte, payload any) error {
		_ = payload
		return errors.New("publish failed")
	}

	app.enqueueSnapshotForPublish(resultsSnapshotEvent{VotingID: "v1", TotalVotes: 7})
	app.flushPendingSnapshots()

	if _, ok := app.dirty["v1"]; !ok {
		t.Fatalf("expected snapshot to be requeued after publish failure")
	}
	if snap, ok := app.pending["v1"]; !ok || snap.TotalVotes != 7 {
		t.Fatalf("expected pending snapshot to be preserved, got %+v exists=%v", snap, ok)
	}
}
