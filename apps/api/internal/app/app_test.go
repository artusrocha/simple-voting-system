package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	configpkg "votingplatform/api/internal/config"
	domain "votingplatform/api/internal/domain"
	"votingplatform/api/internal/logutil"
	service "votingplatform/api/internal/service"
)

func TestValidateVoting(t *testing.T) {
	valid := []domain.Candidate{{CandidateID: "c1", Name: "Alice"}, {CandidateID: "c2", Name: "Bob"}}

	if err := domain.ValidateVoting("Election", valid); err != nil {
		t.Fatalf("expected valid voting, got error: %v", err)
	}

	tests := []struct {
		name       string
		votingName string
		candidates []domain.Candidate
	}{
		{name: "missing name", votingName: " ", candidates: valid},
		{name: "missing candidates", votingName: "Election", candidates: nil},
		{name: "candidate missing id", votingName: "Election", candidates: []domain.Candidate{{CandidateID: "", Name: "Alice"}}},
		{name: "candidate missing name", votingName: "Election", candidates: []domain.Candidate{{CandidateID: "c1", Name: ""}}},
		{name: "duplicate candidate id", votingName: "Election", candidates: []domain.Candidate{{CandidateID: "c1", Name: "Alice"}, {CandidateID: "c1", Name: "Alice 2"}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := domain.ValidateVoting(tc.votingName, tc.candidates); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestValidatePolicy(t *testing.T) {
	valid := domain.PolicyRequest{
		TargetType:    "IP",
		TargetValue:   "198.51.100.10",
		Action:        "ACTIVATE",
		EffectiveMode: "FORWARD_ONLY",
	}

	if err := domain.ValidatePolicy(valid); err != nil {
		t.Fatalf("expected valid policy, got error: %v", err)
	}

	tests := []struct {
		name string
		req  domain.PolicyRequest
	}{
		{name: "invalid target type", req: domain.PolicyRequest{TargetType: "USER", TargetValue: "x", Action: "ACTIVATE", EffectiveMode: "FORWARD_ONLY"}},
		{name: "missing target value", req: domain.PolicyRequest{TargetType: "IP", TargetValue: " ", Action: "ACTIVATE", EffectiveMode: "FORWARD_ONLY"}},
		{name: "invalid action", req: domain.PolicyRequest{TargetType: "IP", TargetValue: "x", Action: "BLOCK", EffectiveMode: "FORWARD_ONLY"}},
		{name: "invalid effective mode", req: domain.PolicyRequest{TargetType: "IP", TargetValue: "x", Action: "ACTIVATE", EffectiveMode: "NOW"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := domain.ValidatePolicy(tc.req); err == nil {
				t.Fatalf("expected validation error")
			}
		})
	}
}

func TestResolveClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{name: "ipv4 with port", remoteAddr: "198.51.100.10:4321", expected: "198.51.100.10"},
		{name: "raw ip", remoteAddr: "198.51.100.10", expected: "198.51.100.10"},
		{name: "ipv6 with port", remoteAddr: "[2001:db8::1]:8080", expected: "2001:db8::1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveClientIP(tc.remoteAddr); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}

func TestRequireEdgeProxyAuthBlocksRequestsWithoutHeader(t *testing.T) {
	app := &application{
		cfg:    configpkg.Config{EdgeProxySharedSecret: "shared-secret", EdgeProxyAuthHeader: "X-App-Edge-Auth"},
		logger: logutil.MustConfigure("api-test", "debug", &bytes.Buffer{}),
	}

	nextCalled := false
	handler := app.requireEdgeProxyAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/votings", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatalf("expected request to be blocked before reaching the next handler")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected HTTP 403, got %d", rec.Code)
	}
}

func TestRequireEdgeProxyAuthAllowsRequestsWithHeader(t *testing.T) {
	app := &application{
		cfg:    configpkg.Config{EdgeProxySharedSecret: "shared-secret", EdgeProxyAuthHeader: "X-App-Edge-Auth"},
		logger: logutil.MustConfigure("api-test", "debug", &bytes.Buffer{}),
	}

	nextCalled := false
	handler := app.requireEdgeProxyAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodPost, "/votings/v1/votes", nil)
	req.Header.Set("X-App-Edge-Auth", "shared-secret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected request to reach the next handler")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}
}

func TestRequireEdgeProxyAuthAllowsHealthzWithoutHeader(t *testing.T) {
	app := &application{
		cfg:    configpkg.Config{EdgeProxySharedSecret: "shared-secret", EdgeProxyAuthHeader: "X-App-Edge-Auth"},
		logger: logutil.MustConfigure("api-test", "debug", &bytes.Buffer{}),
	}

	nextCalled := false
	handler := app.requireEdgeProxyAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatalf("expected healthz request to bypass edge auth")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}
}

func TestRegisterVoteTriggersTemporaryBlockOnHoneypot(t *testing.T) {
	app := &application{
		votings:     map[string]voting{},
		logger:      logutil.MustConfigure("api-test", "debug", &bytes.Buffer{}),
		policyState: service.NewPolicyState(),
		voteStatus:  service.NewVoteStatusStore(),
	}

	app.votings["v1"] = voting{
		VotingID:  "v1",
		Name:      "Election",
		Status:    "OPEN",
		AntiAbuse: antiAbuseConfig{HoneypotEnabled: true, SlideVoteMode: "off", Pow: powConfig{TTLSeconds: 60, BaseDifficultyBits: 18, MaxDifficultyBits: 24, AdaptiveWindowSeconds: 60}},
		Candidates: []domain.Candidate{
			{CandidateID: "c1", Name: "Alice"},
			{CandidateID: "c2", Name: "Bob"},
		},
		CreatedAt: time.Now().UTC(),
	}

	body, err := json.Marshal(voteRequest{CandidateID: "c1", Honeypot: "bot-filled"})
	if err != nil {
		t.Fatalf("marshal vote request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/votings/v1/votes", bytes.NewReader(body))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.registerVote(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for honeypot vote, got %d", rec.Code)
	}

	var response errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode honeypot response: %v", err)
	}
	if response.Code != "honeypot_triggered" {
		t.Fatalf("expected honeypot_triggered code, got %q", response.Code)
	}
	if !app.policyState.IsBlocked("v1", "198.51.100.10") {
		t.Fatalf("expected IP to be blocked after honeypot trigger")
	}

	normalBody, err := json.Marshal(voteRequest{CandidateID: "c1"})
	if err != nil {
		t.Fatalf("marshal normal vote request: %v", err)
	}

	blockedReq := httptest.NewRequest(http.MethodPost, "/votings/v1/votes", bytes.NewReader(normalBody))
	blockedReq.SetPathValue("votingId", "v1")
	blockedReq.RemoteAddr = "198.51.100.10:4321"
	blockedRec := httptest.NewRecorder()

	app.registerVote(blockedRec, blockedReq)

	if blockedRec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for blocked IP, got %d", blockedRec.Code)
	}
	if err := json.Unmarshal(blockedRec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode blocked response: %v", err)
	}
	if response.Code != "blocked_by_policy" {
		t.Fatalf("expected blocked_by_policy code, got %q", response.Code)
	}
}

func TestNormalizeAntiAbuseConfigReturnsDefaults(t *testing.T) {
	app := newPowTestApp()
	got := app.normalizeAntiAbuseConfig(nil)
	if !got.HoneypotEnabled || got.SlideVoteMode != "off" || got.Pow.Algorithm != "sha256" || got.Pow.TTLSeconds != 60 || got.Pow.BaseDifficultyBits != 18 || got.Pow.MaxDifficultyBits != 24 || got.Pow.BaseMemoryKiB != 4096 || got.Pow.MemoryGrowthFactor != 1.2 || got.Pow.DifficultyStepEvery != 4 || got.Pow.MemoryKiB != 8192 || got.Pow.TimeCost != 1 || got.Pow.Parallelism != 1 || got.Pow.HashLength != 32 {
		t.Fatalf("unexpected defaults: %+v", got)
	}
}

func TestMergeAntiAbuseConfigPreservesUntouchedFields(t *testing.T) {
	app := newPowTestApp()
	baseDifficultyBits := 10
	merged := app.mergeAntiAbuseConfig(app.votings["v1"].AntiAbuse, &antiAbuseConfigPatch{
		Pow: &powConfigPatch{BaseDifficultyBits: &baseDifficultyBits},
	})
	if merged.Pow.BaseDifficultyBits != 10 {
		t.Fatalf("expected baseDifficultyBits updated, got %d", merged.Pow.BaseDifficultyBits)
	}
	if !merged.Pow.Enabled {
		t.Fatalf("expected pow.enabled preserved")
	}
	if merged.Pow.Algorithm != "sha256" {
		t.Fatalf("expected pow.algorithm preserved, got %q", merged.Pow.Algorithm)
	}
	if merged.Pow.MaxDifficultyBits != 12 {
		t.Fatalf("expected maxDifficultyBits preserved, got %d", merged.Pow.MaxDifficultyBits)
	}
	if merged.Pow.MemoryKiB != 512 {
		t.Fatalf("expected memoryKiB preserved, got %d", merged.Pow.MemoryKiB)
	}
	if merged.Pow.BaseMemoryKiB != 0 || merged.Pow.MemoryGrowthFactor != 0 || merged.Pow.DifficultyStepEvery != 0 {
		t.Fatalf("expected formula fields preserved as unset, got %+v", merged.Pow)
	}
}

func TestMergeAntiAbuseConfigPreservesFormulaFields(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.Algorithm = "argon2id"
	v.AntiAbuse.Pow.BaseMemoryKiB = 4096
	v.AntiAbuse.Pow.MemoryGrowthFactor = 1.2
	v.AntiAbuse.Pow.DifficultyStepEvery = 4
	app.votings["v1"] = v
	memoryKiB := 2048
	merged := app.mergeAntiAbuseConfig(v.AntiAbuse, &antiAbuseConfigPatch{
		Pow: &powConfigPatch{MemoryKiB: &memoryKiB},
	})
	if merged.Pow.BaseMemoryKiB != 4096 || merged.Pow.MemoryGrowthFactor != 1.2 || merged.Pow.DifficultyStepEvery != 4 {
		t.Fatalf("expected formula fields preserved, got %+v", merged.Pow)
	}
}

func TestCreateVoteChallengeIncludesAlgorithmAndParams(t *testing.T) {
	app := newPowTestApp()
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/vote-challenges", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.createVoteChallenge(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var response voteChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode challenge response: %v", err)
	}
	if response.Algorithm != "sha256" {
		t.Fatalf("expected sha256 algorithm, got %q", response.Algorithm)
	}
	if response.Params.DifficultyBits == 0 {
		t.Fatalf("expected challenge params to include difficulty bits")
	}
	if response.DifficultyBits != response.Params.DifficultyBits {
		t.Fatalf("expected top-level and params difficulty bits to match, got %d and %d", response.DifficultyBits, response.Params.DifficultyBits)
	}
	payload, code, message := app.parseAndValidateChallengeToken(response.Token)
	if code != "" {
		t.Fatalf("parse challenge token failed: %s %s", code, message)
	}
	if payload.Algorithm != "sha256" {
		t.Fatalf("expected token payload algorithm sha256, got %q", payload.Algorithm)
	}
	if payload.Params.DifficultyBits != response.Params.DifficultyBits {
		t.Fatalf("expected token params difficulty bits %d, got %d", response.Params.DifficultyBits, payload.Params.DifficultyBits)
	}
}

func TestCreateVoteChallengeIncludesArgon2Params(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.Algorithm = "argon2id"
	v.AntiAbuse.Pow.MemoryKiB = 64
	v.AntiAbuse.Pow.TimeCost = 1
	v.AntiAbuse.Pow.Parallelism = 1
	v.AntiAbuse.Pow.HashLength = 16
	v.AntiAbuse.Pow.BaseDifficultyBits = 8
	v.AntiAbuse.Pow.MaxDifficultyBits = 8
	app.votings["v1"] = v
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/vote-challenges", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.createVoteChallenge(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var response voteChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode challenge response: %v", err)
	}
	if response.Algorithm != "argon2id" {
		t.Fatalf("expected argon2id algorithm, got %q", response.Algorithm)
	}
	if response.Params.MemoryKiB != 64 || response.Params.TimeCost != 1 || response.Params.Parallelism != 1 || response.Params.HashLength != 16 {
		t.Fatalf("unexpected argon2 params: %+v", response.Params)
	}
}

func TestCreateVoteChallengeUsesArgon2FormulaParams(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.Algorithm = "argon2id"
	v.AntiAbuse.Pow.BaseDifficultyBits = 8
	v.AntiAbuse.Pow.MaxDifficultyBits = 10
	v.AntiAbuse.Pow.BaseMemoryKiB = 4096
	v.AntiAbuse.Pow.MemoryGrowthFactor = 1.2
	v.AntiAbuse.Pow.DifficultyStepEvery = 4
	v.AntiAbuse.Pow.MemoryKiB = 64
	v.AntiAbuse.Pow.TimeCost = 1
	v.AntiAbuse.Pow.Parallelism = 1
	v.AntiAbuse.Pow.HashLength = 16
	app.votings["v1"] = v
	if err := app.antiAbuseStore.RecordVoteAccepted("v1", "198.51.100.10", "session-1", time.Now().UTC(), time.Minute); err != nil {
		t.Fatalf("record first vote activity: %v", err)
	}
	if err := app.antiAbuseStore.RecordVoteAccepted("v1", "198.51.100.10", "session-2", time.Now().UTC(), time.Minute); err != nil {
		t.Fatalf("record second vote activity: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/vote-challenges", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.createVoteChallenge(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	var response voteChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode challenge response: %v", err)
	}
	if response.Params.MemoryKiB != 5120 {
		t.Fatalf("expected formula memoryKiB 5120, got %d", response.Params.MemoryKiB)
	}
	if response.Params.DifficultyBits != 8 {
		t.Fatalf("expected formula difficultyBits 8, got %d", response.Params.DifficultyBits)
	}
}

func TestArgon2FormulaDifficultyBitsIncreaseInSteps(t *testing.T) {
	app := newPowTestApp()
	cfg := powConfig{
		Algorithm:           "argon2id",
		BaseDifficultyBits:  8,
		MaxDifficultyBits:   10,
		BaseMemoryKiB:       4096,
		MemoryGrowthFactor:  1.2,
		DifficultyStepEvery: 4,
	}
	if bits := app.computeArgon2DifficultyBits(0, cfg); bits != 8 {
		t.Fatalf("expected bits 8, got %d", bits)
	}
	if bits := app.computeArgon2DifficultyBits(3, cfg); bits != 8 {
		t.Fatalf("expected bits 8, got %d", bits)
	}
	if bits := app.computeArgon2DifficultyBits(4, cfg); bits != 9 {
		t.Fatalf("expected bits 9, got %d", bits)
	}
	if bits := app.computeArgon2DifficultyBits(9, cfg); bits != 10 {
		t.Fatalf("expected bits clamped at 10, got %d", bits)
	}
}

func TestComputeArgon2MemoryKiBUsesBuckets(t *testing.T) {
	cfg := powConfig{BaseMemoryKiB: 4096, MemoryGrowthFactor: 1.2}
	if got := computeArgon2MemoryKiB(0, cfg); got != 4096 {
		t.Fatalf("expected base memory 4096, got %d", got)
	}
	if got := computeArgon2MemoryKiB(1, cfg); got != 5120 {
		t.Fatalf("expected level 1 memory 5120, got %d", got)
	}
	if got := computeArgon2MemoryKiB(4, cfg); got != 9216 {
		t.Fatalf("expected level 4 memory 9216, got %d", got)
	}
}

func TestValidateAntiAbuseConfigRejectsInvalidFormulaSettings(t *testing.T) {
	app := newPowTestApp()
	cfg := app.normalizeAntiAbuseConfig(&antiAbuseConfig{Pow: powConfig{Algorithm: "argon2id", BaseMemoryKiB: 4096, MemoryGrowthFactor: 0.9, DifficultyStepEvery: 4}})
	if err := app.validateAntiAbuseConfig(cfg); err == nil || err.Error() != "antiAbuse.pow.memoryGrowthFactor must be between 1.0 and 2.0" {
		t.Fatalf("expected memory growth factor validation error, got %v", err)
	}
}

func TestValidateAntiAbuseConfigAllowsLegacyArgon2WithoutFormula(t *testing.T) {
	app := newPowTestApp()
	cfg := app.normalizeAntiAbuseConfig(&antiAbuseConfig{Pow: powConfig{Algorithm: "argon2id", MemoryKiB: 64, TimeCost: 1, Parallelism: 1, HashLength: 16, BaseDifficultyBits: 8, MaxDifficultyBits: 8}})
	if err := app.validateAntiAbuseConfig(cfg); err != nil {
		t.Fatalf("expected legacy argon2 config to remain valid, got %v", err)
	}
}

func TestBuildVoteRawEventIncludesTelemetryAndContext(t *testing.T) {
	app := newPowTestApp()
	now := time.Now().UTC()
	openedAt := now.Add(-3 * time.Second)
	startedAt := now.Add(-2 * time.Second)
	completedAt := now.Add(-1 * time.Second)
	receivedAt := now.Add(-4 * time.Second)
	solveStartedAt := now.Add(-1500 * time.Millisecond)
	solveCompletedAt := now.Add(-500 * time.Millisecond)
	req := voteRequest{
		InteractionSignal: &voteInteractionSignal{
			OpenedAt:          &openedAt,
			StartedAt:         &startedAt,
			CompletedAt:       &completedAt,
			OpenToStartMs:     1000,
			GestureDurationMs: 750,
			MoveEvents:        12,
			MaxProgress:       0.97,
			Completed:         true,
			Mode:              "full",
		},
		PowClientMetrics: &votePowClientMetrics{
			ChallengeID:         "ch-1",
			Algorithm:           "argon2id",
			ChallengeReceivedAt: &receivedAt,
			SolveStartedAt:      &solveStartedAt,
			SolveCompletedAt:    &solveCompletedAt,
			SolveDurationMs:     1000,
			RetryAttempt:        1,
		},
		ClientContext: &voteClientContext{
			Platform:         "Linux x86_64",
			Language:         "en-US",
			Languages:        []string{"en-US", "en"},
			ScreenWidth:      1920,
			ScreenHeight:     1080,
			ViewportWidth:    1440,
			ViewportHeight:   900,
			DevicePixelRatio: 2,
			MaxTouchPoints:   0,
			Timezone:         "UTC",
			Mobile:           false,
		},
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/votings/v1/votes?confirm=true", bytes.NewReader(nil))
	httpReq.Header.Set("User-Agent", "test-agent")
	httpReq.Header.Set("X-Forwarded-For", "198.51.100.10, 10.0.0.1")
	httpReq.AddCookie(&http.Cookie{Name: app.cfg.PowSessionCookieName, Value: "session-1"})
	powIssuedAt := now.Add(-5 * time.Second)
	powExpiresAt := now.Add(30 * time.Second)
	evt := app.buildVoteRawEvent(httpReq, req, "v1", "vote-1", "198.51.100.10", "session-1", true, now, voteChallengeTokenPayload{
		ChallengeID:    "challenge-1",
		Algorithm:      "argon2id",
		DifficultyBits: 9,
		Params:         domain.PowChallengeParams{DifficultyBits: 9, MemoryKiB: 4096, TimeCost: 1, Parallelism: 1, HashLength: 16},
		IssuedAt:       powIssuedAt,
		ExpiresAt:      powExpiresAt,
	}, voteAntiAbuseRuntime{StoreBackend: "memory", ReuseCheckStatus: "ok", VoteActivityRecordStatus: "ok", SessionActivityRecordStatus: "ok"})
	if evt.RequestContext == nil || evt.RequestContext.SessionID != "session-1" || evt.RequestContext.UserAgent != "test-agent" {
		t.Fatalf("unexpected request context: %+v", evt.RequestContext)
	}
	if evt.Client == nil || evt.Client.UserAgent != "test-agent" || evt.Client.Platform != "Linux x86_64" {
		t.Fatalf("unexpected client context: %+v", evt.Client)
	}
	if evt.Interaction == nil || evt.Interaction.MoveEvents != 12 || !evt.Interaction.Completed {
		t.Fatalf("unexpected interaction payload: %+v", evt.Interaction)
	}
	if evt.Pow == nil || evt.Pow.IssueToSubmitMs <= 0 || evt.Pow.SolveDurationMs != 1000 || evt.Pow.RetryAttempt != 1 {
		t.Fatalf("unexpected pow payload: %+v", evt.Pow)
	}
	if evt.AntiAbuseRuntime == nil || evt.AntiAbuseRuntime.ReuseCheckStatus != "ok" {
		t.Fatalf("unexpected anti-abuse runtime: %+v", evt.AntiAbuseRuntime)
	}
}

func TestRegisterVoteIgnoresHoneypotWhenDisabled(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.HoneypotEnabled = false
	v.AntiAbuse.Pow.Enabled = true
	app.votings["v1"] = v
	body, err := json.Marshal(voteRequest{CandidateID: "c1", Honeypot: "filled"})
	if err != nil {
		t.Fatalf("marshal vote request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/votes", bytes.NewReader(body))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.registerVote(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	var response errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Code == "honeypot_triggered" {
		t.Fatalf("expected honeypot to be ignored when disabled")
	}
}

func TestCreateVoteChallengeDisabledForVoting(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.Enabled = false
	app.votings["v1"] = v
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/vote-challenges", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("votingId", "v1")
	rec := httptest.NewRecorder()

	app.createVoteChallenge(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "pow_disabled")
}

func TestCreateVoteChallengeSetsCookie(t *testing.T) {
	app := newPowTestApp()
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/vote-challenges", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.createVoteChallenge(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rec.Code)
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatalf("expected session cookie to be set")
	}
	var response voteChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode challenge response: %v", err)
	}
	if response.ChallengeID == "" || response.Token == "" {
		t.Fatalf("expected challenge id and token in response")
	}
}

func TestRegisterVoteRequiresPowWhenFeatureEnabled(t *testing.T) {
	app := newPowTestApp()
	body, err := json.Marshal(voteRequest{CandidateID: "c1"})
	if err != nil {
		t.Fatalf("marshal vote request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/votings/v1/votes", bytes.NewReader(body))
	req.SetPathValue("votingId", "v1")
	req.RemoteAddr = "198.51.100.10:4321"
	rec := httptest.NewRecorder()

	app.registerVote(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	assertErrorCode(t, rec.Body.Bytes(), "pow_required")
}

func TestValidateVoteProofRejectsMismatchedRouteChallenge(t *testing.T) {
	app := newPowTestApp()
	challenge, sessionID := issuePowChallengeForTest(t, app, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	_, _, code, _ := app.validateVoteProof("v1", "different-challenge", "198.51.100.10", sessionID, proof)
	if code != "pow_invalid" {
		t.Fatalf("expected pow_invalid, got %q", code)
	}
}

func TestValidateVoteProofRejectsExpiredChallenge(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.TTLSeconds = -1
	app.votings["v1"] = v
	challenge := app.issueVoteChallenge("v1", "198.51.100.10", "session-1", v.AntiAbuse.Pow, defaultVoteAntiAbuseRuntime("memory"))
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	_, _, code, _ := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", "session-1", proof)
	if code != "pow_expired" {
		t.Fatalf("expected pow_expired, got %q", code)
	}
}

func TestValidateVoteProofRejectsReuse(t *testing.T) {
	app := newPowTestApp()
	challenge, sessionID := issuePowChallengeForTest(t, app, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	if _, _, code, _ := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof); code != "" {
		t.Fatalf("expected first proof validation to pass, got %q", code)
	}
	if _, _, code, _ := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof); code != "pow_reused" {
		t.Fatalf("expected pow_reused, got %q", code)
	}
}

func TestValidateVoteProofRejectsWrongCookie(t *testing.T) {
	app := newPowTestApp()
	challenge, _ := issuePowChallengeForTest(t, app, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	_, _, code, _ := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", "wrong-session", proof)
	if code != "pow_invalid" {
		t.Fatalf("expected pow_invalid, got %q", code)
	}
}

func TestValidateVoteProofAcceptsArgon2id(t *testing.T) {
	app := newPowTestApp()
	v := app.votings["v1"]
	v.AntiAbuse.Pow.Algorithm = "argon2id"
	v.AntiAbuse.Pow.MemoryKiB = 64
	v.AntiAbuse.Pow.TimeCost = 1
	v.AntiAbuse.Pow.Parallelism = 1
	v.AntiAbuse.Pow.HashLength = 16
	v.AntiAbuse.Pow.BaseDifficultyBits = 8
	v.AntiAbuse.Pow.MaxDifficultyBits = 8
	app.votings["v1"] = v
	challenge, sessionID := issuePowChallengeForTest(t, app, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	if _, _, code, message := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof); code != "" {
		t.Fatalf("expected argon2id proof validation to pass, got %q (%s)", code, message)
	}
}

func TestValidateVoteProofFailsOpenWhenReuseCheckErrors(t *testing.T) {
	app := newPowTestApp()
	app.antiAbuseStore = &failingAntiAbuseStore{markChallengeUsedErr: errAntiAbuseStoreUnavailable}
	challenge, sessionID := issuePowChallengeForTest(t, app, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	payload, runtime, code, message := app.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof)
	if code != "" || message != "" {
		t.Fatalf("expected fail-open validation, got code=%q message=%q", code, message)
	}
	if payload.ChallengeID != challenge.ChallengeID {
		t.Fatalf("expected payload to be preserved, got %+v", payload)
	}
	if runtime.ReuseCheckStatus != "failed_open" {
		t.Fatalf("expected failed_open reuse status, got %+v", runtime)
	}
	if len(runtime.Errors) == 0 || runtime.Errors[0] != "challenge_reuse_check_failed" {
		t.Fatalf("expected reuse check failure recorded, got %+v", runtime.Errors)
	}
}

func TestValidateVoteProofRejectsReuseAcrossApplications(t *testing.T) {
	sharedStore := service.NewMemoryAntiAbuseStore()
	appA := newPowTestAppWithStore(sharedStore)
	appB := newPowTestAppWithStore(sharedStore)
	challenge, sessionID := issuePowChallengeForTest(t, appA, "198.51.100.10")
	proof := voteProof{Token: challenge.Token, Nonce: solveProofNonce(t, challenge.Token)}

	if _, runtime, code, _ := appB.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof); code != "" || runtime.ReuseCheckStatus != "ok" {
		t.Fatalf("expected first cross-instance validation to pass, got code=%q runtime=%+v", code, runtime)
	}
	if _, runtime, code, _ := appA.validateVoteProof("v1", challenge.ChallengeID, "198.51.100.10", sessionID, proof); code != "pow_reused" || runtime.ReuseCheckStatus != "reused" {
		t.Fatalf("expected cross-instance reuse to be blocked, got code=%q runtime=%+v", code, runtime)
	}
}

func TestComputePowLevelSharesLoadAcrossSessionsForSameIP(t *testing.T) {
	app := newPowTestApp()
	now := time.Now().UTC()
	window := 60 * time.Second
	if err := app.antiAbuseStore.RecordVoteAccepted("v1", "198.51.100.10", "session-a", now, window); err != nil {
		t.Fatalf("record session a vote: %v", err)
	}
	if err := app.antiAbuseStore.RecordVoteAccepted("v1", "198.51.100.10", "session-b", now, window); err != nil {
		t.Fatalf("record session b vote: %v", err)
	}
	level := app.computePowLevel("v1", "198.51.100.10", app.votings["v1"].AntiAbuse.Pow)
	if level != 1 {
		t.Fatalf("expected shared IP load level 1, got %d", level)
	}
}

func newPowTestApp() *application {
	return newPowTestAppWithStore(service.NewMemoryAntiAbuseStore())
}

func newPowTestAppWithStore(store service.AntiAbuseStore) *application {
	app := &application{
		cfg: configpkg.Config{
			FeaturePowVote:           true,
			PowSecret:                "test-secret",
			PowTTLSeconds:            20,
			PowAlgorithm:             "sha256",
			PowBaseDifficultyBits:    8,
			PowMaxDifficultyBits:     12,
			PowAdaptiveWindowSeconds: 60,
			PowArgon2MemoryKiB:       8192,
			PowArgon2TimeCost:        1,
			PowArgon2Parallelism:     1,
			PowArgon2HashLength:      32,
			PowSessionCookieName:     "pow_session",
		},
		votings:        map[string]voting{},
		logger:         logutil.MustConfigure("api-test", "debug", &bytes.Buffer{}),
		policyState:    service.NewPolicyState(),
		voteStatus:     service.NewVoteStatusStore(),
		antiAbuseStore: store,
	}
	app.votings["v1"] = voting{
		VotingID: "v1",
		Name:     "Election",
		Status:   "OPEN",
		AntiAbuse: antiAbuseConfig{
			HoneypotEnabled:             true,
			SlideVoteMode:               "full",
			InteractionTelemetryEnabled: true,
			Pow: powConfig{
				Enabled:               true,
				Algorithm:             "sha256",
				TTLSeconds:            20,
				BaseDifficultyBits:    8,
				MaxDifficultyBits:     12,
				AdaptiveWindowSeconds: 60,
				MemoryKiB:             512,
				TimeCost:              1,
				Parallelism:           1,
				HashLength:            32,
			},
		},
		Candidates: []domain.Candidate{
			{CandidateID: "c1", Name: "Alice"},
			{CandidateID: "c2", Name: "Bob"},
		},
		CreatedAt: time.Now().UTC(),
	}
	return app
}

var errAntiAbuseStoreUnavailable = errors.New("anti-abuse store unavailable")

type failingAntiAbuseStore struct {
	markChallengeUsedErr error
	recordChallengeErr   error
	recordVoteErr        error
}

func (s *failingAntiAbuseStore) MarkChallengeUsed(string, time.Time) (bool, error) {
	if s.markChallengeUsedErr != nil {
		return false, s.markChallengeUsedErr
	}
	return true, nil
}

func (s *failingAntiAbuseStore) RecordChallengeIssued(string, string, string, time.Time, time.Duration) error {
	return s.recordChallengeErr
}

func (s *failingAntiAbuseStore) RecordVoteAccepted(string, string, string, time.Time, time.Duration) error {
	return s.recordVoteErr
}

func (s *failingAntiAbuseStore) CountRecentVotesByIP(string, string, time.Time) (int, error) {
	return 0, nil
}

func (s *failingAntiAbuseStore) CountRecentChallengesByIP(string, string, time.Time) (int, error) {
	return 0, nil
}

func (s *failingAntiAbuseStore) CountRecentSessionActivity(string, string, string, time.Time) (int, error) {
	return 0, nil
}

func (s *failingAntiAbuseStore) Close() error { return nil }

func issuePowChallengeForTest(t *testing.T, app *application, ip string) (voteChallengeResponse, string) {
	t.Helper()
	sessionID := "session-1"
	challenge := app.issueVoteChallenge("v1", ip, sessionID, app.votings["v1"].AntiAbuse.Pow, defaultVoteAntiAbuseRuntime("memory"))
	return challenge, sessionID
}

func solveProofNonce(t *testing.T, token string) string {
	t.Helper()
	payload, code, message := newPowTestApp().parseAndValidateChallengeToken(token)
	if code != "" {
		t.Fatalf("parse challenge token failed: %s %s", code, message)
	}
	for i := uint64(0); i < 1_000_000; i++ {
		nonce := strconv.FormatUint(i, 10)
		if verifyPowSolution(payload, nonce) {
			return nonce
		}
	}
	t.Fatal("failed to solve proof nonce in test")
	return ""
}

func assertErrorCode(t *testing.T, body []byte, expected string) {
	t.Helper()
	var response errorResponse
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response.Code != expected {
		t.Fatalf("expected code %q, got %q", expected, response.Code)
	}
}
