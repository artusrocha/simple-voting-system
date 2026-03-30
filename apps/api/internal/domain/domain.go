package domain

import (
	"errors"
	"strings"
	"time"
)

type Candidate struct {
	CandidateID string `json:"candidateId"`
	Name        string `json:"name"`
}

type Voting struct {
	VotingID   string          `json:"votingId"`
	Name       string          `json:"name"`
	Status     string          `json:"status"`
	Candidates []Candidate     `json:"candidates"`
	StartsAt   *time.Time      `json:"startsAt,omitempty"`
	EndsAt     *time.Time      `json:"endsAt,omitempty"`
	AntiAbuse  AntiAbuseConfig `json:"antiAbuse"`
	CreatedAt  time.Time       `json:"createdAt"`
	UpdatedAt  *time.Time      `json:"updatedAt,omitempty"`
}

type AntiAbuseConfig struct {
	HoneypotEnabled             bool      `json:"honeypotEnabled"`
	SlideVoteMode               string    `json:"slideVoteMode"`
	InteractionTelemetryEnabled bool      `json:"interactionTelemetryEnabled"`
	Pow                         PowConfig `json:"pow"`
}

type PowConfig struct {
	Enabled               bool    `json:"enabled"`
	Algorithm             string  `json:"algorithm,omitempty"`
	TTLSeconds            int     `json:"ttlSeconds"`
	BaseDifficultyBits    int     `json:"baseDifficultyBits"`
	MaxDifficultyBits     int     `json:"maxDifficultyBits"`
	AdaptiveWindowSeconds int     `json:"adaptiveWindowSeconds"`
	BaseMemoryKiB         int     `json:"baseMemoryKiB,omitempty"`
	MemoryGrowthFactor    float64 `json:"memoryGrowthFactor,omitempty"`
	DifficultyStepEvery   int     `json:"difficultyStepEvery,omitempty"`
	MemoryKiB             int     `json:"memoryKiB,omitempty"`
	TimeCost              int     `json:"timeCost,omitempty"`
	Parallelism           int     `json:"parallelism,omitempty"`
	HashLength            int     `json:"hashLength,omitempty"`
}

type AntiAbuseConfigPatch struct {
	HoneypotEnabled             *bool           `json:"honeypotEnabled,omitempty"`
	SlideVoteMode               *string         `json:"slideVoteMode,omitempty"`
	InteractionTelemetryEnabled *bool           `json:"interactionTelemetryEnabled,omitempty"`
	Pow                         *PowConfigPatch `json:"pow,omitempty"`
}

type PowConfigPatch struct {
	Enabled               *bool    `json:"enabled,omitempty"`
	Algorithm             *string  `json:"algorithm,omitempty"`
	TTLSeconds            *int     `json:"ttlSeconds,omitempty"`
	BaseDifficultyBits    *int     `json:"baseDifficultyBits,omitempty"`
	MaxDifficultyBits     *int     `json:"maxDifficultyBits,omitempty"`
	AdaptiveWindowSeconds *int     `json:"adaptiveWindowSeconds,omitempty"`
	BaseMemoryKiB         *int     `json:"baseMemoryKiB,omitempty"`
	MemoryGrowthFactor    *float64 `json:"memoryGrowthFactor,omitempty"`
	DifficultyStepEvery   *int     `json:"difficultyStepEvery,omitempty"`
	MemoryKiB             *int     `json:"memoryKiB,omitempty"`
	TimeCost              *int     `json:"timeCost,omitempty"`
	Parallelism           *int     `json:"parallelism,omitempty"`
	HashLength            *int     `json:"hashLength,omitempty"`
}

type CreateVotingRequest struct {
	Name       string           `json:"name"`
	Status     *string          `json:"status,omitempty"`
	Candidates []Candidate      `json:"candidates"`
	StartsAt   *time.Time       `json:"startsAt,omitempty"`
	EndsAt     *time.Time       `json:"endsAt,omitempty"`
	AntiAbuse  *AntiAbuseConfig `json:"antiAbuse,omitempty"`
}

type UpdateVotingRequest struct {
	Name       *string               `json:"name,omitempty"`
	Status     *string               `json:"status,omitempty"`
	StartsAt   *time.Time            `json:"startsAt,omitempty"`
	EndsAt     *time.Time            `json:"endsAt,omitempty"`
	Candidates *[]Candidate          `json:"candidates,omitempty"`
	AntiAbuse  *AntiAbuseConfigPatch `json:"antiAbuse,omitempty"`
}

type VoteRequest struct {
	CandidateID       string                 `json:"candidateId"`
	IP                string                 `json:"ip,omitempty"`
	Honeypot          string                 `json:"honeypot,omitempty"`
	Pow               *VoteProof             `json:"pow,omitempty"`
	InteractionSignal *VoteInteractionSignal `json:"interactionSignal,omitempty"`
	PowClientMetrics  *VotePowClientMetrics  `json:"powClientMetrics,omitempty"`
	ClientContext     *VoteClientContext     `json:"clientContext,omitempty"`
}

type VoteProof struct {
	Token string `json:"token"`
	Nonce string `json:"nonce"`
}

type VoteInteractionSignal struct {
	OpenedAt          *time.Time `json:"openedAt,omitempty"`
	StartedAt         *time.Time `json:"startedAt,omitempty"`
	CompletedAt       *time.Time `json:"completedAt,omitempty"`
	OpenToStartMs     int        `json:"openToStartMs,omitempty"`
	GestureDurationMs int        `json:"gestureDurationMs,omitempty"`
	MoveEvents        int        `json:"moveEvents,omitempty"`
	MaxProgress       float64    `json:"maxProgress,omitempty"`
	Completed         bool       `json:"completed,omitempty"`
	Cancelled         bool       `json:"cancelled,omitempty"`
	Mode              string     `json:"mode,omitempty"`
}

type VotePowClientMetrics struct {
	ChallengeID         string     `json:"challengeId,omitempty"`
	Algorithm           string     `json:"algorithm,omitempty"`
	ChallengeReceivedAt *time.Time `json:"challengeReceivedAt,omitempty"`
	SolveStartedAt      *time.Time `json:"solveStartedAt,omitempty"`
	SolveCompletedAt    *time.Time `json:"solveCompletedAt,omitempty"`
	SubmitStartedAt     *time.Time `json:"submitStartedAt,omitempty"`
	SolveDurationMs     int        `json:"solveDurationMs,omitempty"`
	RetryAttempt        int        `json:"retryAttempt,omitempty"`
}

type VoteClientContext struct {
	UserAgent        string   `json:"userAgent,omitempty"`
	Platform         string   `json:"platform,omitempty"`
	Language         string   `json:"language,omitempty"`
	Languages        []string `json:"languages,omitempty"`
	ScreenWidth      int      `json:"screenWidth,omitempty"`
	ScreenHeight     int      `json:"screenHeight,omitempty"`
	ViewportWidth    int      `json:"viewportWidth,omitempty"`
	ViewportHeight   int      `json:"viewportHeight,omitempty"`
	DevicePixelRatio float64  `json:"devicePixelRatio,omitempty"`
	MaxTouchPoints   int      `json:"maxTouchPoints,omitempty"`
	Timezone         string   `json:"timezone,omitempty"`
	Mobile           bool     `json:"mobile,omitempty"`
}

type VotePowDetails struct {
	ChallengeID         string             `json:"challengeId,omitempty"`
	Algorithm           string             `json:"algorithm,omitempty"`
	DifficultyBits      int                `json:"difficultyBits,omitempty"`
	Params              PowChallengeParams `json:"params,omitempty"`
	Validated           bool               `json:"validated,omitempty"`
	IssuedAt            *time.Time         `json:"issuedAt,omitempty"`
	ExpiresAt           *time.Time         `json:"expiresAt,omitempty"`
	ChallengeReceivedAt *time.Time         `json:"challengeReceivedAt,omitempty"`
	SolveStartedAt      *time.Time         `json:"solveStartedAt,omitempty"`
	SolveCompletedAt    *time.Time         `json:"solveCompletedAt,omitempty"`
	SubmittedAt         *time.Time         `json:"submittedAt,omitempty"`
	SolveDurationMs     int                `json:"solveDurationMs,omitempty"`
	IssueToSubmitMs     int                `json:"issueToSubmitMs,omitempty"`
	RetryAttempt        int                `json:"retryAttempt,omitempty"`
}

type VoteRequestContext struct {
	IP           string    `json:"ip,omitempty"`
	SessionID    string    `json:"sessionId,omitempty"`
	UserAgent    string    `json:"userAgent,omitempty"`
	ForwardedFor string    `json:"forwardedFor,omitempty"`
	ReceivedAt   time.Time `json:"receivedAt"`
	Confirm      bool      `json:"confirm,omitempty"`
}

type VoteAntiAbuseRuntime struct {
	StoreBackend                string   `json:"storeBackend,omitempty"`
	ReuseCheckStatus            string   `json:"reuseCheckStatus,omitempty"`
	ChallengeIssueRecordStatus  string   `json:"challengeIssueRecordStatus,omitempty"`
	VoteActivityRecordStatus    string   `json:"voteActivityRecordStatus,omitempty"`
	SessionActivityRecordStatus string   `json:"sessionActivityRecordStatus,omitempty"`
	Errors                      []string `json:"errors,omitempty"`
}

type PowChallengeParams struct {
	DifficultyBits int `json:"difficultyBits,omitempty"`
	MemoryKiB      int `json:"memoryKiB,omitempty"`
	TimeCost       int `json:"timeCost,omitempty"`
	Parallelism    int `json:"parallelism,omitempty"`
	HashLength     int `json:"hashLength,omitempty"`
}

type VoteChallengeResponse struct {
	ChallengeID    string             `json:"challengeId"`
	Token          string             `json:"token"`
	Algorithm      string             `json:"algorithm"`
	DifficultyBits int                `json:"difficultyBits,omitempty"`
	Params         PowChallengeParams `json:"params,omitempty"`
	ExpiresAt      time.Time          `json:"expiresAt"`
}

type ResultsResponse struct {
	VotingID              string             `json:"votingId"`
	TotalVotes            int64              `json:"totalVotes"`
	ByCandidate           map[string]int64   `json:"byCandidate"`
	PercentageByCandidate map[string]float64 `json:"percentageByCandidate"`
	ByHour                map[string]int64   `json:"byHour"`
	Checkpoint            string             `json:"checkpoint,omitempty"`
	UpdatedAt             time.Time          `json:"updatedAt"`
}

type ReplayMetadata struct {
	InitialOffsetsByPartition       map[string]int64 `json:"initialOffsetsByPartition,omitempty"`
	LastProcessedOffsetsByPartition map[string]int64 `json:"lastProcessedOffsetsByPartition,omitempty"`
}

type ResultsSnapshotEvent struct {
	VotingID              string             `json:"votingId"`
	TotalVotes            int64              `json:"totalVotes"`
	ByCandidate           map[string]int64   `json:"byCandidate"`
	PercentageByCandidate map[string]float64 `json:"percentageByCandidate"`
	ByHour                map[string]int64   `json:"byHour"`
	Checkpoint            string             `json:"checkpoint,omitempty"`
	UpdatedAt             time.Time          `json:"updatedAt"`
	ReplayMetadata        ReplayMetadata     `json:"replayMetadata,omitempty"`
}

func PublicResultsFromSnapshot(snap ResultsSnapshotEvent) ResultsResponse {
	return ResultsResponse{
		VotingID:              snap.VotingID,
		TotalVotes:            snap.TotalVotes,
		ByCandidate:           cloneInt64Map(snap.ByCandidate),
		PercentageByCandidate: cloneFloat64Map(snap.PercentageByCandidate),
		ByHour:                cloneInt64Map(snap.ByHour),
		Checkpoint:            snap.Checkpoint,
		UpdatedAt:             snap.UpdatedAt,
	}
}

func cloneInt64Map(values map[string]int64) map[string]int64 {
	if values == nil {
		return nil
	}
	out := make(map[string]int64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

func cloneFloat64Map(values map[string]float64) map[string]float64 {
	if values == nil {
		return nil
	}
	out := make(map[string]float64, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}

type PolicyRequest struct {
	TargetType    string `json:"targetType"`
	TargetValue   string `json:"targetValue"`
	Action        string `json:"action"`
	EffectiveMode string `json:"effectiveMode"`
	Reason        string `json:"reason,omitempty"`
}

type PolicyResponse struct {
	PolicyEventID string    `json:"policyEventId"`
	VotingID      string    `json:"votingId"`
	TargetType    string    `json:"targetType"`
	TargetValue   string    `json:"targetValue"`
	Action        string    `json:"action"`
	EffectiveMode string    `json:"effectiveMode"`
	CreatedAt     time.Time `json:"createdAt"`
}

type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type VotingCatalogEvent struct {
	EventID    string    `json:"eventId"`
	OccurredAt time.Time `json:"occurredAt"`
	Voting     Voting    `json:"voting"`
}

type VoteRawEvent struct {
	VoteID            string                 `json:"voteId"`
	VotingID          string                 `json:"votingId"`
	CandidateID       string                 `json:"candidateId"`
	OccurredAt        time.Time              `json:"occurredAt"`
	IP                string                 `json:"ip"`
	PowChallengeID    string                 `json:"powChallengeId,omitempty"`
	PowDifficultyBits int                    `json:"powDifficultyBits,omitempty"`
	PowValidated      bool                   `json:"powValidated,omitempty"`
	PowIssuedAt       time.Time              `json:"powIssuedAt,omitempty"`
	PowExpiresAt      time.Time              `json:"powExpiresAt,omitempty"`
	Pow               *VotePowDetails        `json:"pow,omitempty"`
	Interaction       *VoteInteractionSignal `json:"interaction,omitempty"`
	Client            *VoteClientContext     `json:"client,omitempty"`
	RequestContext    *VoteRequestContext    `json:"requestContext,omitempty"`
	AntiAbuseRuntime  *VoteAntiAbuseRuntime  `json:"antiAbuseRuntime,omitempty"`
}

type PolicyControlEvent struct {
	PolicyEventID string    `json:"policyEventId"`
	VotingID      string    `json:"votingId"`
	TargetType    string    `json:"targetType"`
	TargetValue   string    `json:"targetValue"`
	Action        string    `json:"action"`
	EffectiveMode string    `json:"effectiveMode"`
	Reason        string    `json:"reason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

type PolicyLatestEvent struct {
	VotingID    string    `json:"votingId"`
	TargetValue string    `json:"targetValue"`
	Active      bool      `json:"active"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func ValidateVoting(name string, candidates []Candidate) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("name is required")
	}
	if len(candidates) == 0 {
		return errors.New("at least one candidate is required")
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, c := range candidates {
		if strings.TrimSpace(c.CandidateID) == "" {
			return errors.New("candidate id is required")
		}
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("candidate name is required")
		}
		if _, exists := seen[c.CandidateID]; exists {
			return errors.New("candidate ids must be unique")
		}
		seen[c.CandidateID] = struct{}{}
	}
	return nil
}

func ValidatePolicy(req PolicyRequest) error {
	if req.TargetType != "IP" {
		return errors.New("only targetType IP is supported")
	}
	if strings.TrimSpace(req.TargetValue) == "" {
		return errors.New("targetValue is required")
	}
	if req.Action != "ACTIVATE" && req.Action != "DEACTIVATE" {
		return errors.New("action must be ACTIVATE or DEACTIVATE")
	}
	if req.EffectiveMode != "FORWARD_ONLY" && req.EffectiveMode != "RETROACTIVE" {
		return errors.New("effectiveMode must be FORWARD_ONLY or RETROACTIVE")
	}
	return nil
}

func HasCandidate(candidates []Candidate, candidateID string) bool {
	for _, c := range candidates {
		if c.CandidateID == candidateID {
			return true
		}
	}
	return false
}
