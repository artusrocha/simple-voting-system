package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	configpkg "votingplatform/api/internal/config"
	domain "votingplatform/api/internal/domain"
	httpapi "votingplatform/api/internal/httpapi"
	"votingplatform/api/internal/kafkautil"
	"votingplatform/api/internal/logutil"
	service "votingplatform/api/internal/service"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/crypto/argon2"
)

type candidate = domain.Candidate
type voting = domain.Voting
type createVotingRequest = domain.CreateVotingRequest
type updateVotingRequest = domain.UpdateVotingRequest
type antiAbuseConfig = domain.AntiAbuseConfig
type powConfig = domain.PowConfig
type antiAbuseConfigPatch = domain.AntiAbuseConfigPatch
type powConfigPatch = domain.PowConfigPatch
type voteRequest = domain.VoteRequest
type voteProof = domain.VoteProof
type voteInteractionSignal = domain.VoteInteractionSignal
type votePowClientMetrics = domain.VotePowClientMetrics
type voteClientContext = domain.VoteClientContext
type votePowDetails = domain.VotePowDetails
type voteRequestContext = domain.VoteRequestContext
type voteAntiAbuseRuntime = domain.VoteAntiAbuseRuntime
type voteChallengeResponse = domain.VoteChallengeResponse
type resultsResponse = domain.ResultsResponse
type resultsSnapshotEvent = domain.ResultsSnapshotEvent
type policyRequest = domain.PolicyRequest
type policyResponse = domain.PolicyResponse
type errorResponse = domain.ErrorResponse
type votingCatalogEvent = domain.VotingCatalogEvent
type voteRawEvent = domain.VoteRawEvent
type policyControlEvent = domain.PolicyControlEvent
type policyLatestEvent = domain.PolicyLatestEvent

type voteAcceptedResponse struct {
	Status   string `json:"status"`
	VoteID   string `json:"voteId"`
	VotingID string `json:"votingId"`
	Message  string `json:"message"`
}

type voteStatusResponse struct {
	VoteID    string    `json:"voteId"`
	VotingID  string    `json:"votingId"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type voteChallengeTokenPayload struct {
	ChallengeID      string                    `json:"challengeId"`
	VotingID         string                    `json:"votingId"`
	IssuedAt         time.Time                 `json:"issuedAt"`
	ExpiresAt        time.Time                 `json:"expiresAt"`
	Algorithm        string                    `json:"algorithm"`
	DifficultyBits   int                       `json:"difficultyBits"`
	Params           domain.PowChallengeParams `json:"params,omitempty"`
	Salt             string                    `json:"salt"`
	IPHash           string                    `json:"ipHash"`
	SessionHash      string                    `json:"sessionHash"`
	AntiAbuseRuntime voteAntiAbuseRuntime      `json:"antiAbuseRuntime,omitempty"`
}

type application struct {
	cfg    configpkg.Config
	logger *slog.Logger

	votingsMu sync.RWMutex
	votings   map[string]voting

	policyState *service.PolicyState

	snapshotMu sync.RWMutex
	snapshots  map[string]resultsResponse

	voteStatus     *service.VoteStatusStore
	antiAbuseStore service.AntiAbuseStore

	votingProducer  *kafka.Producer
	voteProducers   []*kafka.Producer
	voteProducerIdx atomic.Int64
	policyProducer  *kafka.Producer
}

var (
	temporaryBlockDuration = time.Hour

	votesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "votes_received_total",
		Help: "Total vote requests received.",
	})
	votesBlocked = promauto.NewCounter(prometheus.CounterOpts{
		Name: "votes_blocked_total",
		Help: "Total vote requests blocked by policy.",
	})
	votesBlockedByHoneypot = promauto.NewCounter(prometheus.CounterOpts{
		Name: "votes_blocked_by_honeypot_total",
		Help: "Total vote requests blocked after honeypot detection.",
	})
	temporaryBlocksApplied = promauto.NewCounter(prometheus.CounterOpts{
		Name: "temporary_blocks_applied_total",
		Help: "Total temporary IP blocks applied due to abuse signals.",
	})
	votesAccepted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "votes_accepted_total",
		Help: "Total vote requests accepted.",
	})
	votePublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vote_publish_errors_total",
		Help: "Total vote publish failures.",
	})
	voteDeliverySuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vote_delivery_success_total",
		Help: "Total votes successfully delivered to Kafka.",
	})
	voteDeliveryErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "vote_delivery_errors_total",
		Help: "Total votes failed to deliver to Kafka.",
	})
	powChallengesIssued = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pow_challenges_issued_total",
		Help: "Total proof-of-work challenges issued.",
	})
	powChallengesValidated = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pow_challenges_valid_total",
		Help: "Total proof-of-work challenges validated successfully.",
	})
	powChallengesInvalid = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pow_challenges_invalid_total",
		Help: "Total invalid proof-of-work validations.",
	})
	powChallengesExpired = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pow_challenges_expired_total",
		Help: "Total expired proof-of-work challenges received.",
	})
	powChallengesReused = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pow_challenges_reused_total",
		Help: "Total reused proof-of-work challenges received.",
	})
	powDifficultyBits = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pow_difficulty_bits",
		Help:    "Difficulty bits issued for proof-of-work challenges.",
		Buckets: []float64{20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30},
	})
)

func Run() {
	cfg := configpkg.Load()
	logger := logutil.MustConfigure("api", cfg.LogLevel, nil)

	numProducers := 4
	producers := make([]*kafka.Producer, numProducers)
	for i := 0; i < numProducers; i++ {
		producers[i] = kafkautil.NewProducer(cfg.Brokers)
	}
	antiAbuseStore, err := service.NewAntiAbuseStore(cfg)
	if err != nil {
		logger.Error("failed to initialize anti-abuse store", "error", err)
		panic(err)
	}

	app := &application{
		cfg:            cfg,
		logger:         logger,
		votings:        make(map[string]voting),
		policyState:    service.NewPolicyState(),
		snapshots:      make(map[string]resultsResponse),
		voteStatus:     service.NewVoteStatusStore(),
		antiAbuseStore: antiAbuseStore,
		voteProducers:  producers,
		votingProducer: kafkautil.NewProducer(cfg.Brokers),
		policyProducer: kafkautil.NewProducer(cfg.Brokers),
	}

	for i := 0; i < numProducers; i++ {
		go app.handleDeliveryReports(producers[i])
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.consumeTopic(ctx, cfg.TopicVotingCatalog, app.handleCatalogLatest)
	app.consumeTopic(ctx, cfg.TopicVotingPolicyLatest, app.handlePolicyLatest)
	app.consumeTopic(ctx, cfg.TopicResultsSnapshot, app.handleSnapshot)

	mux := httpapi.NewMux(httpapi.Handlers{
		Healthz:             app.healthz,
		CreateVoting:        app.createVoting,
		ListVotings:         app.listVotings,
		GetVoting:           app.getVoting,
		PatchVoting:         app.patchVoting,
		CreateVoteChallenge: app.createVoteChallenge,
		RegisterVote:        app.registerVote,
		GetResults:          app.getResults,
		CreatePolicy:        app.createPolicy,
		GetVoteStatus:       app.getVoteStatus,
	})

	handler := httpapi.Instrument(httpapi.WithCORS(mux))
	if cfg.EdgeProxySharedSecret != "" {
		handler = app.requireEdgeProxyAuth(handler)
	}

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("api server failed", "error", err)
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	cancel()
	_ = server.Shutdown(shutdownCtx)
	for _, p := range app.voteProducers {
		p.Flush(5_000)
		p.Close()
	}
	app.votingProducer.Flush(5_000)
	app.policyProducer.Flush(5_000)
	app.votingProducer.Close()
	app.policyProducer.Close()
	if app.antiAbuseStore != nil {
		_ = app.antiAbuseStore.Close()
	}
}

func (app *application) consumeTopic(ctx context.Context, topic string, handler func(*kafka.Message)) {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": strings.Join(app.cfg.Brokers, ","),
		"group.id":          kafkautil.UniqueGroupID("api"),
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		app.logger.Error("failed to create consumer", "topic", topic, "error", err)
		panic(err)
	}

	if err := consumer.Subscribe(topic, nil); err != nil {
		app.logger.Error("failed to subscribe to topic", "topic", topic, "error", err)
		panic(err)
	}

	go func() {
		defer consumer.Close()
		for {
			if ctx.Err() != nil {
				return
			}

			msg, err := consumer.ReadMessage(500 * time.Millisecond)
			if err != nil {
				if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.IsTimeout() {
					continue
				}
				app.logger.Warn("consumer read error", "topic", topic, "error", err)
				time.Sleep(2 * time.Second)
				continue
			}
			handler(msg)
		}
	}()
}

func (app *application) handleDeliveryReports(producer *kafka.Producer) {
	for e := range producer.Events() {
		switch ev := e.(type) {
		case *kafka.Message:
			voteID := string(ev.Key)
			if voteID == "" {
				continue
			}

			updated := app.voteStatus.Update(voteID, func(entry service.VoteStatusEntry) service.VoteStatusEntry {
				if ev.TopicPartition.Error != nil {
					entry.Status = service.VoteStatusFailed
					voteDeliveryErrors.Inc()
					app.logger.Error("vote delivery failed", "voteId", voteID, "error", ev.TopicPartition.Error)
				} else {
					entry.Status = service.VoteStatusWritten
					voteDeliverySuccess.Inc()
					app.logger.Debug("vote delivery confirmed", "voteId", voteID)
				}
				entry.UpdatedAt = time.Now().UTC()
				return entry
			})
			if !updated {
				continue
			}

			app.cleanupVoteStatus()
		}
	}
}

func (app *application) getNextProducer() *kafka.Producer {
	idx := app.voteProducerIdx.Add(1) % int64(len(app.voteProducers))
	return app.voteProducers[idx]
}

func (app *application) cleanupVoteStatus() {
	app.voteStatus.CleanupOlderThan(time.Now().UTC().Add(-1 * time.Minute))
}

func (app *application) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *application) requireEdgeProxyAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		if subtleConstantTimeEquals(r.Header.Get(app.cfg.EdgeProxyAuthHeader), app.cfg.EdgeProxySharedSecret) {
			next.ServeHTTP(w, r)
			return
		}

		app.logger.Warn("request blocked without edge proxy authentication", "path", r.URL.Path, "method", r.Method, "remoteAddr", r.RemoteAddr)
		writeError(w, http.StatusForbidden, "edge_proxy_required", "request must pass through the configured edge proxy")
	})
}

func (app *application) createVoting(w http.ResponseWriter, r *http.Request) {
	var req createVotingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := validateVoting(req.Name, req.Candidates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	now := time.Now().UTC()
	status := "CREATED"
	if req.Status != nil {
		if slices.Contains([]string{"CREATED", "OPEN", "CLOSED", "CANCELLED"}, *req.Status) {
			status = *req.Status
		}
	}
	v := voting{
		VotingID:   newULID(),
		Name:       strings.TrimSpace(req.Name),
		Status:     status,
		Candidates: req.Candidates,
		StartsAt:   req.StartsAt,
		EndsAt:     req.EndsAt,
		AntiAbuse:  app.normalizeAntiAbuseConfig(req.AntiAbuse),
		CreatedAt:  now,
	}

	if err := app.validateAntiAbuseConfig(v.AntiAbuse); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	evt := votingCatalogEvent{EventID: newULID(), OccurredAt: now, Voting: v}
	if err := kafkautil.PublishJSON(app.votingProducer, app.cfg.TopicVotingsCatalog, []byte(v.VotingID), evt); err != nil {
		writeError(w, http.StatusInternalServerError, "kafka_publish_failed", "failed to persist voting")
		return
	}

	app.votingsMu.Lock()
	app.votings[v.VotingID] = v
	app.votingsMu.Unlock()

	writeJSON(w, http.StatusCreated, v)
}

func (app *application) listVotings(w http.ResponseWriter, r *http.Request) {
	statusFilter := r.URL.Query().Get("status")

	app.votingsMu.RLock()
	out := make([]voting, 0, len(app.votings))
	for _, v := range app.votings {
		if statusFilter != "" && v.Status != statusFilter {
			continue
		}
		out = append(out, v)
	}
	app.votingsMu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, out)
}

func (app *application) getVoting(w http.ResponseWriter, r *http.Request) {
	votingID := r.PathValue("votingId")
	app.votingsMu.RLock()
	v, ok := app.votings[votingID]
	app.votingsMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "voting_not_found", "voting not found")
		return
	}

	writeJSON(w, http.StatusOK, v)
}

func (app *application) patchVoting(w http.ResponseWriter, r *http.Request) {
	votingID := r.PathValue("votingId")

	var req updateVotingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	app.votingsMu.Lock()
	v, ok := app.votings[votingID]
	if !ok {
		app.votingsMu.Unlock()
		writeError(w, http.StatusNotFound, "voting_not_found", "voting not found")
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			app.votingsMu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_request", "name cannot be empty")
			return
		}
		v.Name = name
	}
	if req.Status != nil {
		if !slices.Contains([]string{"CREATED", "OPEN", "CLOSED", "CANCELLED"}, *req.Status) {
			app.votingsMu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_request", "invalid status")
			return
		}
		v.Status = *req.Status
	}
	if req.StartsAt != nil {
		v.StartsAt = req.StartsAt
	}
	if req.EndsAt != nil {
		v.EndsAt = req.EndsAt
	}
	if req.Candidates != nil {
		if err := validateVoting(v.Name, *req.Candidates); err != nil {
			app.votingsMu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
		v.Candidates = *req.Candidates
	}
	if req.AntiAbuse != nil {
		v.AntiAbuse = app.mergeAntiAbuseConfig(v.AntiAbuse, req.AntiAbuse)
		if err := app.validateAntiAbuseConfig(v.AntiAbuse); err != nil {
			app.votingsMu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}

	now := time.Now().UTC()
	v.UpdatedAt = &now
	app.votings[votingID] = v
	app.votingsMu.Unlock()

	evt := votingCatalogEvent{EventID: newULID(), OccurredAt: now, Voting: v}
	if err := kafkautil.PublishJSON(app.votingProducer, app.cfg.TopicVotingsCatalog, []byte(v.VotingID), evt); err != nil {
		writeError(w, http.StatusInternalServerError, "kafka_publish_failed", "failed to persist voting")
		return
	}

	writeJSON(w, http.StatusOK, v)
}

func (app *application) registerVote(w http.ResponseWriter, r *http.Request) {
	votesReceived.Inc()
	votingID := r.PathValue("votingId")
	challengeID := strings.TrimSpace(r.PathValue("challengeId"))

	var req voteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.CandidateID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "candidateId is required")
		return
	}

	v, ok := app.getOpenVoting(votingID, w)
	if !ok {
		return
	}

	if !hasCandidate(v.Candidates, req.CandidateID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "candidateId is not part of voting")
		return
	}

	ip := app.resolveRequestIP(r, strings.TrimSpace(req.IP))

	antiAbuse := v.AntiAbuse

	if antiAbuse.HoneypotEnabled && strings.TrimSpace(req.Honeypot) != "" {
		app.applyTemporaryBlock(votingID, ip, req.Honeypot)
		votesBlocked.Inc()
		votesBlockedByHoneypot.Inc()
		writeError(w, http.StatusForbidden, "honeypot_triggered", "vote blocked by anti-bot protection")
		return
	}

	if app.policyState.IsBlocked(votingID, ip) {
		votesBlocked.Inc()
		app.logger.Warn("vote blocked by policy", "votingId", votingID, "ip", ip, "candidateId", req.CandidateID)
		writeError(w, http.StatusForbidden, "blocked_by_policy", "vote blocked by active policy")
		return
	}

	sessionID := app.peekPowSessionID(r)
	var powPayload voteChallengeTokenPayload
	antiAbuseRuntime := defaultVoteAntiAbuseRuntime(app.cfg.AntiAbuseStore)
	if app.isPowEnabledForVoting(antiAbuse) {
		if req.Pow == nil || strings.TrimSpace(req.Pow.Token) == "" || strings.TrimSpace(req.Pow.Nonce) == "" {
			writeError(w, http.StatusForbidden, "pow_required", "proof-of-work is required")
			return
		}
		if challengeID == "" {
			writeError(w, http.StatusBadRequest, "pow_invalid", "challengeId is required in route")
			return
		}
		resolvedSessionID, err := app.getPowSessionID(r)
		if err != nil {
			powChallengesInvalid.Inc()
			writeError(w, http.StatusForbidden, "pow_invalid", "missing proof-of-work session")
			return
		}
		sessionID = resolvedSessionID
		payload, runtimeStatus, code, message := app.validateVoteProof(votingID, challengeID, ip, sessionID, *req.Pow)
		if code != "" {
			if code == "pow_expired" {
				powChallengesExpired.Inc()
			} else if code == "pow_reused" {
				powChallengesReused.Inc()
			} else {
				powChallengesInvalid.Inc()
			}
			writeError(w, http.StatusForbidden, code, message)
			return
		}
		powChallengesValidated.Inc()
		powPayload = payload
		antiAbuseRuntime = mergeVoteAntiAbuseRuntime(antiAbuseRuntime, runtimeStatus)
	}

	confirm := r.URL.Query().Get("confirm") == "true"
	var timeoutMs int = 5000
	if confirm {
		if t := r.URL.Query().Get("timeoutMs"); t != "" {
			fmt.Sscanf(t, "%d", &timeoutMs)
			if timeoutMs > 30000 {
				timeoutMs = 30000
			}
		}
	}

	now := time.Now().UTC()
	voteID := newULID()

	app.voteStatus.Set(voteID, service.VoteStatusEntry{
		VotingID:  votingID,
		Status:    service.VoteStatusPending,
		UpdatedAt: now,
	})

	if app.antiAbuseStore != nil {
		window := time.Duration(antiAbuse.Pow.AdaptiveWindowSeconds) * time.Second
		if err := app.antiAbuseStore.RecordVoteAccepted(votingID, ip, sessionID, now, window); err != nil {
			app.logger.Warn("failed to record vote activity", "votingId", votingID, "ip", ip, "error", err)
			antiAbuseRuntime.VoteActivityRecordStatus = "failed"
			antiAbuseRuntime.SessionActivityRecordStatus = "failed"
			antiAbuseRuntime.Errors = append(antiAbuseRuntime.Errors, "vote_activity_record_failed")
		} else {
			antiAbuseRuntime.VoteActivityRecordStatus = "ok"
			antiAbuseRuntime.SessionActivityRecordStatus = "ok"
		}
	}
	evt := app.buildVoteRawEvent(r, req, votingID, voteID, ip, sessionID, confirm, now, powPayload, antiAbuseRuntime)

	producer := app.getNextProducer()
	if err := kafkautil.PublishJSON(producer, app.cfg.TopicVotesRaw, []byte(voteID), evt); err != nil {
		app.voteStatus.Delete(voteID)
		votePublishErrors.Inc()
		writeError(w, http.StatusInternalServerError, "kafka_publish_failed", "failed to persist vote")
		return
	}
	if confirm {
		deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
		for time.Now().Before(deadline) {
			entry, exists := app.voteStatus.Get(voteID)

			if !exists {
				break
			}

			if entry.Status != service.VoteStatusPending {
				votesAccepted.Inc()
				resp := voteAcceptedResponse{
					Status:   string(entry.Status),
					VoteID:   voteID,
					VotingID: votingID,
					Message:  "vote " + string(entry.Status),
				}
				if entry.Status == service.VoteStatusWritten {
					writeJSON(w, http.StatusAccepted, resp)
				} else {
					writeError(w, http.StatusInternalServerError, "vote_delivery_failed", "vote failed to persist")
				}
				return
			}

			time.Sleep(10 * time.Millisecond)
		}

		entry, _ := app.voteStatus.Get(voteID)
		votesAccepted.Inc()
		resp := voteAcceptedResponse{
			Status:   string(entry.Status),
			VoteID:   voteID,
			VotingID: votingID,
			Message:  "vote status: " + string(entry.Status) + " (timeout waiting for confirmation)",
		}
		writeJSON(w, http.StatusAccepted, resp)
		return
	}

	votesAccepted.Inc()
	resp := voteAcceptedResponse{
		Status:   "PENDING",
		VoteID:   voteID,
		VotingID: votingID,
		Message:  "vote accepted for asynchronous processing",
	}
	writeJSON(w, http.StatusAccepted, resp)
}

func (app *application) createVoteChallenge(w http.ResponseWriter, r *http.Request) {
	votingID := r.PathValue("votingId")
	v, ok := app.getOpenVoting(votingID, w)
	if !ok {
		return
	}
	if !app.isPowEnabledForVoting(v.AntiAbuse) {
		writeError(w, http.StatusNotFound, "pow_disabled", "proof-of-work is disabled for this voting")
		return
	}

	ip := app.resolveRequestIP(r, "")
	sessionID := app.ensurePowSessionCookie(w, r)
	runtime := defaultVoteAntiAbuseRuntime(app.cfg.AntiAbuseStore)
	if app.antiAbuseStore != nil {
		window := time.Duration(v.AntiAbuse.Pow.AdaptiveWindowSeconds) * time.Second
		if err := app.antiAbuseStore.RecordChallengeIssued(votingID, ip, sessionID, time.Now().UTC(), window); err != nil {
			app.logger.Warn("failed to record challenge activity", "votingId", votingID, "ip", ip, "error", err)
			runtime.ChallengeIssueRecordStatus = "failed"
			runtime.SessionActivityRecordStatus = "failed"
			runtime.Errors = append(runtime.Errors, "challenge_issue_record_failed")
		} else {
			runtime.ChallengeIssueRecordStatus = "ok"
			runtime.SessionActivityRecordStatus = "ok"
		}
	}
	challenge := app.issueVoteChallenge(votingID, ip, sessionID, v.AntiAbuse.Pow, runtime)
	powChallengesIssued.Inc()
	powDifficultyBits.Observe(float64(challenge.DifficultyBits))
	writeJSON(w, http.StatusCreated, challenge)
}

func (app *application) applyTemporaryBlock(votingID, ip, honeypotValue string) {
	until := time.Now().UTC().Add(temporaryBlockDuration)
	app.policyState.SetBlockedUntil(votingID, ip, until)
	temporaryBlocksApplied.Inc()
	app.logger.Warn("temporary block applied", "votingId", votingID, "ip", ip, "reason", "honeypot_triggered", "blockedUntil", until, "honeypotValue", honeypotValue)
}

func (app *application) defaultAntiAbuseConfig() antiAbuseConfig {
	return antiAbuseConfig{
		HoneypotEnabled:             true,
		SlideVoteMode:               "off",
		InteractionTelemetryEnabled: false,
		Pow: powConfig{
			Enabled:               false,
			Algorithm:             normalizePowAlgorithm(app.cfg.PowAlgorithm),
			TTLSeconds:            60,
			BaseDifficultyBits:    18,
			MaxDifficultyBits:     24,
			AdaptiveWindowSeconds: 60,
			BaseMemoryKiB:         4096,
			MemoryGrowthFactor:    1.2,
			DifficultyStepEvery:   4,
			MemoryKiB:             app.cfg.PowArgon2MemoryKiB,
			TimeCost:              app.cfg.PowArgon2TimeCost,
			Parallelism:           app.cfg.PowArgon2Parallelism,
			HashLength:            app.cfg.PowArgon2HashLength,
		},
	}
}

func (app *application) normalizeAntiAbuseConfig(cfg *antiAbuseConfig) antiAbuseConfig {
	defaults := app.defaultAntiAbuseConfig()
	if cfg == nil {
		return defaults
	}
	out := *cfg
	out.SlideVoteMode = strings.ToLower(strings.TrimSpace(out.SlideVoteMode))
	if out.SlideVoteMode == "" {
		out.SlideVoteMode = defaults.SlideVoteMode
	}
	if out.Pow.TTLSeconds == 0 {
		out.Pow.TTLSeconds = defaults.Pow.TTLSeconds
	}
	out.Pow.Algorithm = strings.ToLower(strings.TrimSpace(out.Pow.Algorithm))
	if out.Pow.Algorithm == "" {
		out.Pow.Algorithm = defaults.Pow.Algorithm
	}
	if out.Pow.BaseDifficultyBits == 0 {
		out.Pow.BaseDifficultyBits = defaults.Pow.BaseDifficultyBits
	}
	if out.Pow.MaxDifficultyBits == 0 {
		out.Pow.MaxDifficultyBits = defaults.Pow.MaxDifficultyBits
	}
	if out.Pow.AdaptiveWindowSeconds == 0 {
		out.Pow.AdaptiveWindowSeconds = defaults.Pow.AdaptiveWindowSeconds
	}
	if out.Pow.BaseMemoryKiB == 0 && out.Pow.MemoryGrowthFactor > 0 && out.Pow.DifficultyStepEvery > 0 {
		out.Pow.BaseMemoryKiB = defaults.Pow.BaseMemoryKiB
	}
	if out.Pow.MemoryGrowthFactor == 0 && out.Pow.BaseMemoryKiB > 0 && out.Pow.DifficultyStepEvery > 0 {
		out.Pow.MemoryGrowthFactor = defaults.Pow.MemoryGrowthFactor
	}
	if out.Pow.DifficultyStepEvery == 0 && out.Pow.BaseMemoryKiB > 0 && out.Pow.MemoryGrowthFactor > 0 {
		out.Pow.DifficultyStepEvery = defaults.Pow.DifficultyStepEvery
	}
	if out.Pow.MemoryKiB == 0 {
		out.Pow.MemoryKiB = defaults.Pow.MemoryKiB
	}
	if out.Pow.TimeCost == 0 {
		out.Pow.TimeCost = defaults.Pow.TimeCost
	}
	if out.Pow.Parallelism == 0 {
		out.Pow.Parallelism = defaults.Pow.Parallelism
	}
	if out.Pow.HashLength == 0 {
		out.Pow.HashLength = defaults.Pow.HashLength
	}
	return out
}

func (app *application) mergeAntiAbuseConfig(current antiAbuseConfig, patch *antiAbuseConfigPatch) antiAbuseConfig {
	merged := current
	if patch == nil {
		return merged
	}
	if patch.HoneypotEnabled != nil {
		merged.HoneypotEnabled = *patch.HoneypotEnabled
	}
	if patch.SlideVoteMode != nil {
		merged.SlideVoteMode = strings.TrimSpace(*patch.SlideVoteMode)
	}
	if patch.InteractionTelemetryEnabled != nil {
		merged.InteractionTelemetryEnabled = *patch.InteractionTelemetryEnabled
	}
	if patch.Pow != nil {
		if patch.Pow.Enabled != nil {
			merged.Pow.Enabled = *patch.Pow.Enabled
		}
		if patch.Pow.Algorithm != nil {
			merged.Pow.Algorithm = *patch.Pow.Algorithm
		}
		if patch.Pow.TTLSeconds != nil {
			merged.Pow.TTLSeconds = *patch.Pow.TTLSeconds
		}
		if patch.Pow.BaseDifficultyBits != nil {
			merged.Pow.BaseDifficultyBits = *patch.Pow.BaseDifficultyBits
		}
		if patch.Pow.MaxDifficultyBits != nil {
			merged.Pow.MaxDifficultyBits = *patch.Pow.MaxDifficultyBits
		}
		if patch.Pow.AdaptiveWindowSeconds != nil {
			merged.Pow.AdaptiveWindowSeconds = *patch.Pow.AdaptiveWindowSeconds
		}
		if patch.Pow.BaseMemoryKiB != nil {
			merged.Pow.BaseMemoryKiB = *patch.Pow.BaseMemoryKiB
		}
		if patch.Pow.MemoryGrowthFactor != nil {
			merged.Pow.MemoryGrowthFactor = *patch.Pow.MemoryGrowthFactor
		}
		if patch.Pow.DifficultyStepEvery != nil {
			merged.Pow.DifficultyStepEvery = *patch.Pow.DifficultyStepEvery
		}
		if patch.Pow.MemoryKiB != nil {
			merged.Pow.MemoryKiB = *patch.Pow.MemoryKiB
		}
		if patch.Pow.TimeCost != nil {
			merged.Pow.TimeCost = *patch.Pow.TimeCost
		}
		if patch.Pow.Parallelism != nil {
			merged.Pow.Parallelism = *patch.Pow.Parallelism
		}
		if patch.Pow.HashLength != nil {
			merged.Pow.HashLength = *patch.Pow.HashLength
		}
	}
	return app.normalizeAntiAbuseConfig(&merged)
}

func (app *application) validateAntiAbuseConfig(cfg antiAbuseConfig) error {
	if !slices.Contains([]string{"off", "button", "full"}, strings.ToLower(strings.TrimSpace(cfg.SlideVoteMode))) {
		return errors.New("antiAbuse.slideVoteMode must be off, button, or full")
	}
	if cfg.Pow.TTLSeconds < 10 || cfg.Pow.TTLSeconds > 300 {
		return errors.New("antiAbuse.pow.ttlSeconds must be between 10 and 300")
	}
	algorithm := normalizePowAlgorithm(cfg.Pow.Algorithm)
	if !slices.Contains([]string{"sha256", "argon2id"}, algorithm) {
		return errors.New("antiAbuse.pow.algorithm must be sha256 or argon2id")
	}
	if cfg.Pow.BaseDifficultyBits < 8 || cfg.Pow.BaseDifficultyBits > 30 {
		return errors.New("antiAbuse.pow.baseDifficultyBits must be between 8 and 30")
	}
	if cfg.Pow.MaxDifficultyBits < 8 || cfg.Pow.MaxDifficultyBits > 30 {
		return errors.New("antiAbuse.pow.maxDifficultyBits must be between 8 and 30")
	}
	if cfg.Pow.BaseDifficultyBits > cfg.Pow.MaxDifficultyBits {
		return errors.New("antiAbuse.pow.baseDifficultyBits must be less than or equal to antiAbuse.pow.maxDifficultyBits")
	}
	if cfg.Pow.AdaptiveWindowSeconds < 10 || cfg.Pow.AdaptiveWindowSeconds > 3600 {
		return errors.New("antiAbuse.pow.adaptiveWindowSeconds must be between 10 and 3600")
	}
	formulaConfigured := cfg.Pow.BaseMemoryKiB > 0 || cfg.Pow.MemoryGrowthFactor > 0 || cfg.Pow.DifficultyStepEvery > 0
	if formulaConfigured {
		if cfg.Pow.BaseMemoryKiB < 8 || cfg.Pow.BaseMemoryKiB > 262144 {
			return errors.New("antiAbuse.pow.baseMemoryKiB must be between 8 and 262144")
		}
		if cfg.Pow.MemoryGrowthFactor < 1.0 || cfg.Pow.MemoryGrowthFactor > 2.0 {
			return errors.New("antiAbuse.pow.memoryGrowthFactor must be between 1.0 and 2.0")
		}
		if cfg.Pow.DifficultyStepEvery < 1 || cfg.Pow.DifficultyStepEvery > 20 {
			return errors.New("antiAbuse.pow.difficultyStepEvery must be between 1 and 20")
		}
	}
	if cfg.Pow.MemoryKiB < 8 || cfg.Pow.MemoryKiB > 262144 {
		return errors.New("antiAbuse.pow.memoryKiB must be between 8 and 262144")
	}
	if cfg.Pow.TimeCost < 1 || cfg.Pow.TimeCost > 10 {
		return errors.New("antiAbuse.pow.timeCost must be between 1 and 10")
	}
	if cfg.Pow.Parallelism < 1 || cfg.Pow.Parallelism > 16 {
		return errors.New("antiAbuse.pow.parallelism must be between 1 and 16")
	}
	if cfg.Pow.HashLength < 16 || cfg.Pow.HashLength > 64 {
		return errors.New("antiAbuse.pow.hashLength must be between 16 and 64")
	}
	return nil
}

func (app *application) isPowEnabledForVoting(cfg antiAbuseConfig) bool {
	return app.cfg.FeaturePowVote && cfg.Pow.Enabled
}

func (app *application) getOpenVoting(votingID string, w http.ResponseWriter) (voting, bool) {
	app.votingsMu.RLock()
	v, ok := app.votings[votingID]
	app.votingsMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "voting_not_found", "voting not found")
		return voting{}, false
	}
	if v.Status != "OPEN" {
		writeError(w, http.StatusConflict, "invalid_voting_state", "voting is not OPEN")
		return voting{}, false
	}
	now := time.Now().UTC()
	if v.StartsAt != nil && now.Before(*v.StartsAt) {
		writeError(w, http.StatusBadRequest, "voting_not_started", "voting has not started yet")
		return voting{}, false
	}
	if v.EndsAt != nil && now.After(*v.EndsAt) {
		writeError(w, http.StatusBadRequest, "voting_ended", "voting has ended")
		return voting{}, false
	}
	return v, true
}

func (app *application) resolveRequestIP(r *http.Request, override string) string {
	ip := strings.TrimSpace(override)
	if ip != "" {
		return ip
	}
	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 {
			ip = strings.TrimSpace(parts[0])
		}
	}
	if ip == "" {
		ip = resolveClientIP(r.RemoteAddr)
	}
	if ip == "" {
		return "unknown"
	}
	return ip
}

func (app *application) ensurePowSessionCookie(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(app.cfg.PowSessionCookieName); err == nil {
		value := strings.TrimSpace(cookie.Value)
		if value != "" {
			return value
		}
	}
	value := newULID()
	http.SetCookie(w, &http.Cookie{
		Name:     app.cfg.PowSessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})
	return value
}

func (app *application) getPowSessionID(r *http.Request) (string, error) {
	cookie, err := r.Cookie(app.cfg.PowSessionCookieName)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(cookie.Value)
	if value == "" {
		return "", errors.New("empty session cookie")
	}
	return value, nil
}

func (app *application) peekPowSessionID(r *http.Request) string {
	value, err := app.getPowSessionID(r)
	if err != nil {
		return ""
	}
	return value
}

func (app *application) buildVoteRawEvent(r *http.Request, req voteRequest, votingID, voteID, ip, sessionID string, confirm bool, now time.Time, powPayload voteChallengeTokenPayload, antiAbuseRuntime voteAntiAbuseRuntime) voteRawEvent {
	forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	userAgent := strings.TrimSpace(r.UserAgent())
	evt := voteRawEvent{
		VoteID:      voteID,
		VotingID:    votingID,
		CandidateID: req.CandidateID,
		OccurredAt:  now,
		IP:          ip,
		Client:      sanitizeVoteClientContext(req.ClientContext, userAgent),
		RequestContext: &voteRequestContext{
			IP:           ip,
			SessionID:    sessionID,
			UserAgent:    userAgent,
			ForwardedFor: forwardedFor,
			ReceivedAt:   now,
			Confirm:      confirm,
		},
		AntiAbuseRuntime: normalizeVoteAntiAbuseRuntime(antiAbuseRuntime),
	}
	if req.InteractionSignal != nil {
		interaction := *req.InteractionSignal
		evt.Interaction = &interaction
	}
	if powPayload.ChallengeID != "" {
		evt.PowChallengeID = powPayload.ChallengeID
		evt.PowDifficultyBits = powPayload.DifficultyBits
		evt.PowValidated = true
		evt.PowIssuedAt = powPayload.IssuedAt
		evt.PowExpiresAt = powPayload.ExpiresAt
		issueToSubmitMs := int(now.Sub(powPayload.IssuedAt) / time.Millisecond)
		if issueToSubmitMs < 0 {
			issueToSubmitMs = 0
		}
		pow := votePowDetails{
			ChallengeID:     powPayload.ChallengeID,
			Algorithm:       powPayload.Algorithm,
			DifficultyBits:  powPayload.DifficultyBits,
			Params:          powPayload.Params,
			Validated:       true,
			IssuedAt:        timePtr(powPayload.IssuedAt),
			ExpiresAt:       timePtr(powPayload.ExpiresAt),
			SubmittedAt:     timePtr(now),
			IssueToSubmitMs: issueToSubmitMs,
		}
		if req.PowClientMetrics != nil {
			pow.ChallengeReceivedAt = req.PowClientMetrics.ChallengeReceivedAt
			pow.SolveStartedAt = req.PowClientMetrics.SolveStartedAt
			pow.SolveCompletedAt = req.PowClientMetrics.SolveCompletedAt
			pow.SolveDurationMs = max(req.PowClientMetrics.SolveDurationMs, 0)
			pow.RetryAttempt = max(req.PowClientMetrics.RetryAttempt, 0)
		}
		evt.Pow = &pow
	}
	return evt
}

func sanitizeVoteClientContext(ctx *voteClientContext, fallbackUserAgent string) *voteClientContext {
	if ctx == nil && fallbackUserAgent == "" {
		return nil
	}
	if ctx == nil {
		return &voteClientContext{UserAgent: fallbackUserAgent}
	}
	out := *ctx
	if strings.TrimSpace(out.UserAgent) == "" {
		out.UserAgent = fallbackUserAgent
	}
	return &out
}

func timePtr(value time.Time) *time.Time {
	t := value.UTC()
	return &t
}

func defaultVoteAntiAbuseRuntime(storeBackend string) voteAntiAbuseRuntime {
	return voteAntiAbuseRuntime{StoreBackend: strings.TrimSpace(storeBackend)}
}

func mergeVoteAntiAbuseRuntime(base, next voteAntiAbuseRuntime) voteAntiAbuseRuntime {
	if next.StoreBackend != "" {
		base.StoreBackend = next.StoreBackend
	}
	if next.ReuseCheckStatus != "" {
		base.ReuseCheckStatus = next.ReuseCheckStatus
	}
	if next.ChallengeIssueRecordStatus != "" {
		base.ChallengeIssueRecordStatus = next.ChallengeIssueRecordStatus
	}
	if next.VoteActivityRecordStatus != "" {
		base.VoteActivityRecordStatus = next.VoteActivityRecordStatus
	}
	if next.SessionActivityRecordStatus != "" {
		base.SessionActivityRecordStatus = next.SessionActivityRecordStatus
	}
	if len(next.Errors) > 0 {
		base.Errors = append(base.Errors, next.Errors...)
	}
	return base
}

func normalizeVoteAntiAbuseRuntime(runtime voteAntiAbuseRuntime) *voteAntiAbuseRuntime {
	if runtime.StoreBackend == "" && runtime.ReuseCheckStatus == "" && runtime.ChallengeIssueRecordStatus == "" && runtime.VoteActivityRecordStatus == "" && runtime.SessionActivityRecordStatus == "" && len(runtime.Errors) == 0 {
		return nil
	}
	if len(runtime.Errors) > 1 {
		seen := make(map[string]struct{}, len(runtime.Errors))
		deduped := runtime.Errors[:0]
		for _, item := range runtime.Errors {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			deduped = append(deduped, item)
		}
		runtime.Errors = deduped
	}
	return &runtime
}

func (app *application) issueVoteChallenge(votingID, ip, sessionID string, cfg powConfig, antiAbuseRuntime voteAntiAbuseRuntime) voteChallengeResponse {
	now := time.Now().UTC()
	expiresAt := now.Add(time.Duration(cfg.TTLSeconds) * time.Second)
	algorithm := normalizePowAlgorithm(cfg.Algorithm)
	params := app.buildPowChallengeParams(votingID, ip, cfg)
	payload := voteChallengeTokenPayload{
		ChallengeID:      newULID(),
		VotingID:         votingID,
		IssuedAt:         now,
		ExpiresAt:        expiresAt,
		Algorithm:        algorithm,
		DifficultyBits:   params.DifficultyBits,
		Params:           params,
		Salt:             newULID(),
		IPHash:           app.hashValue(ip),
		SessionHash:      app.hashValue(sessionID),
		AntiAbuseRuntime: antiAbuseRuntime,
	}
	token := app.signChallengePayload(payload)
	return voteChallengeResponse{
		ChallengeID:    payload.ChallengeID,
		Token:          token,
		Algorithm:      algorithm,
		DifficultyBits: payload.DifficultyBits,
		Params:         params,
		ExpiresAt:      payload.ExpiresAt,
	}
}

func normalizePowAlgorithm(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "sha256"
	}
	return value
}

func (app *application) buildPowChallengeParams(votingID, ip string, cfg powConfig) domain.PowChallengeParams {
	level := app.computePowLevel(votingID, ip, cfg)
	switch normalizePowAlgorithm(cfg.Algorithm) {
	case "sha256":
		return domain.PowChallengeParams{
			DifficultyBits: app.computePowDifficultyBits(level, cfg),
		}
	case "argon2id":
		difficultyBits := app.computeArgon2DifficultyBits(level, cfg)
		memoryKiB := cfg.MemoryKiB
		if app.isArgon2FormulaConfigured(cfg) {
			memoryKiB = computeArgon2MemoryKiB(level, cfg)
		}
		return domain.PowChallengeParams{
			DifficultyBits: difficultyBits,
			MemoryKiB:      memoryKiB,
			TimeCost:       cfg.TimeCost,
			Parallelism:    cfg.Parallelism,
			HashLength:     cfg.HashLength,
		}
	default:
		return domain.PowChallengeParams{}
	}
}

func (app *application) computePowLevel(votingID, ip string, cfg powConfig) int {
	window := time.Duration(cfg.AdaptiveWindowSeconds) * time.Second
	votesLastMinute := 0
	if app.antiAbuseStore != nil {
		count, err := app.antiAbuseStore.CountRecentVotesByIP(votingID, ip, time.Now().UTC().Add(-window))
		if err != nil {
			app.logger.Warn("failed to count anti-abuse activity", "votingId", votingID, "ip", ip, "error", err)
		} else {
			votesLastMinute = count
		}
	}
	extra := 0
	if votesLastMinute > 1 {
		extra = 1 << (votesLastMinute - 2)
	}
	return extra
}

func (app *application) computePowDifficultyBits(level int, cfg powConfig) int {
	bits := cfg.BaseDifficultyBits + max(level, 0)
	if bits > cfg.MaxDifficultyBits {
		bits = cfg.MaxDifficultyBits
	}
	return bits
}

func (app *application) computeArgon2DifficultyBits(level int, cfg powConfig) int {
	if !app.isArgon2FormulaConfigured(cfg) {
		return app.computePowDifficultyBits(level, cfg)
	}
	stepEvery := max(cfg.DifficultyStepEvery, 1)
	bits := cfg.BaseDifficultyBits + max(level, 0)/stepEvery
	if bits > cfg.MaxDifficultyBits {
		bits = cfg.MaxDifficultyBits
	}
	return bits
}

func (app *application) isArgon2FormulaConfigured(cfg powConfig) bool {
	return cfg.BaseMemoryKiB > 0 && cfg.MemoryGrowthFactor > 0 && cfg.DifficultyStepEvery > 0
}

func computeArgon2MemoryKiB(level int, cfg powConfig) int {
	base := max(cfg.BaseMemoryKiB, 8)
	growthFactor := cfg.MemoryGrowthFactor
	if growthFactor <= 0 {
		return base
	}
	scaled := float64(base) * math.Pow(growthFactor, float64(max(level, 0)))
	bucketed := bucketMemoryKiB(int(math.Ceil(scaled)))
	return min(bucketed, 262144)
}

func bucketMemoryKiB(value int) int {
	if value <= 0 {
		return 1024
	}
	const bucketSize = 1024
	return ((value + bucketSize - 1) / bucketSize) * bucketSize
}

func (app *application) hashValue(value string) string {
	sum := sha256.Sum256([]byte(value + ":" + app.cfg.PowSecret))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func (app *application) signChallengePayload(payload voteChallengeTokenPayload) string {
	encodedPayload := mustEncodeJSONBase64(payload)
	mac := hmac.New(sha256.New, []byte(app.cfg.PowSecret))
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature
}

func (app *application) parseAndValidateChallengeToken(token string) (voteChallengeTokenPayload, string, string) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return voteChallengeTokenPayload{}, "pow_signature_invalid", "invalid proof-of-work token"
	}
	mac := hmac.New(sha256.New, []byte(app.cfg.PowSecret))
	mac.Write([]byte(parts[0]))
	expectedSignature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expectedSignature), []byte(parts[1])) {
		return voteChallengeTokenPayload{}, "pow_signature_invalid", "invalid proof-of-work signature"
	}
	rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return voteChallengeTokenPayload{}, "pow_invalid", "invalid proof-of-work payload"
	}
	var payload voteChallengeTokenPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		return voteChallengeTokenPayload{}, "pow_invalid", "invalid proof-of-work payload"
	}
	return payload, "", ""
}

func (app *application) validateVoteProof(votingID, challengeID, ip, sessionID string, proof voteProof) (voteChallengeTokenPayload, voteAntiAbuseRuntime, string, string) {
	runtime := defaultVoteAntiAbuseRuntime(app.cfg.AntiAbuseStore)
	payload, code, message := app.parseAndValidateChallengeToken(proof.Token)
	if code != "" {
		return voteChallengeTokenPayload{}, runtime, code, message
	}
	runtime = mergeVoteAntiAbuseRuntime(runtime, payload.AntiAbuseRuntime)
	if payload.VotingID != votingID {
		return voteChallengeTokenPayload{}, runtime, "pow_invalid", "proof-of-work voting mismatch"
	}
	if payload.ChallengeID != challengeID {
		return voteChallengeTokenPayload{}, runtime, "pow_invalid", "proof-of-work challenge mismatch"
	}
	now := time.Now().UTC()
	if now.After(payload.ExpiresAt) {
		return voteChallengeTokenPayload{}, runtime, "pow_expired", "proof-of-work challenge expired"
	}
	if payload.IPHash != app.hashValue(ip) || payload.SessionHash != app.hashValue(sessionID) {
		return voteChallengeTokenPayload{}, runtime, "pow_invalid", "proof-of-work binding mismatch"
	}
	if !verifyPowSolution(payload, proof.Nonce) {
		return voteChallengeTokenPayload{}, runtime, "pow_invalid", "invalid proof-of-work solution"
	}
	if app.antiAbuseStore == nil {
		runtime.ReuseCheckStatus = "failed_open"
		runtime.Errors = append(runtime.Errors, "challenge_reuse_check_skipped")
		return payload, runtime, "", ""
	}
	marked, err := app.antiAbuseStore.MarkChallengeUsed(challengeID, payload.ExpiresAt)
	if err != nil {
		app.logger.Warn("failed to mark challenge as used", "challengeId", challengeID, "error", err)
		runtime.ReuseCheckStatus = "failed_open"
		runtime.Errors = append(runtime.Errors, "challenge_reuse_check_failed")
		return payload, runtime, "", ""
	}
	if !marked {
		runtime.ReuseCheckStatus = "reused"
		return voteChallengeTokenPayload{}, runtime, "pow_reused", "proof-of-work challenge already used"
	}
	runtime.ReuseCheckStatus = "ok"
	return payload, runtime, "", ""
}

func verifyPowSolution(payload voteChallengeTokenPayload, nonce string) bool {
	switch normalizePowAlgorithm(payload.Algorithm) {
	case "sha256":
		if _, err := strconv.ParseUint(strings.TrimSpace(nonce), 10, 64); err != nil {
			return false
		}
		material := payload.ChallengeID + ":" + payload.VotingID + ":" + payload.Salt + ":" + strings.TrimSpace(nonce)
		sum := sha256.Sum256([]byte(material))
		bits := payload.Params.DifficultyBits
		if bits == 0 {
			bits = payload.DifficultyBits
		}
		return hasLeadingZeroBits(sum[:], bits)
	case "argon2id":
		if _, err := strconv.ParseUint(strings.TrimSpace(nonce), 10, 64); err != nil {
			return false
		}
		material := payload.ChallengeID + ":" + payload.VotingID + ":" + payload.Salt + ":" + strings.TrimSpace(nonce)
		digest := argon2.IDKey([]byte(material), []byte(payload.Salt), uint32(max(payload.Params.TimeCost, 1)), uint32(max(payload.Params.MemoryKiB, 8)), uint8(max(payload.Params.Parallelism, 1)), uint32(max(payload.Params.HashLength, 16)))
		bits := payload.Params.DifficultyBits
		if bits == 0 {
			bits = payload.DifficultyBits
		}
		return hasLeadingZeroBits(digest, bits)
	default:
		return false
	}
}

func max(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

func min(value, ceiling int) int {
	if value > ceiling {
		return ceiling
	}
	return value
}

func hasLeadingZeroBits(hash []byte, bits int) bool {
	if bits <= 0 {
		return true
	}
	fullBytes := bits / 8
	remainingBits := bits % 8
	for i := 0; i < fullBytes; i++ {
		if i >= len(hash) || hash[i] != 0 {
			return false
		}
	}
	if remainingBits == 0 {
		return true
	}
	if fullBytes >= len(hash) {
		return false
	}
	mask := byte(0xFF << (8 - remainingBits))
	return hash[fullBytes]&mask == 0
}

func mustEncodeJSONBase64(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func (app *application) getResults(w http.ResponseWriter, r *http.Request) {
	votingID := r.PathValue("votingId")

	app.votingsMu.RLock()
	v, ok := app.votings[votingID]
	app.votingsMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "voting_not_found", "voting not found")
		return
	}

	app.snapshotMu.RLock()
	result, hasSnapshot := app.snapshots[votingID]
	app.snapshotMu.RUnlock()

	if !hasSnapshot {
		zeroCandidate := make(map[string]int64, len(v.Candidates))
		zeroPct := make(map[string]float64, len(v.Candidates))
		for _, c := range v.Candidates {
			zeroCandidate[c.CandidateID] = 0
			zeroPct[c.CandidateID] = 0
		}
		result = resultsResponse{
			VotingID:              votingID,
			TotalVotes:            0,
			ByCandidate:           zeroCandidate,
			PercentageByCandidate: zeroPct,
			ByHour:                map[string]int64{},
			UpdatedAt:             time.Now().UTC(),
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (app *application) getVoteStatus(w http.ResponseWriter, r *http.Request) {
	voteID := r.PathValue("voteId")

	entry, exists := app.voteStatus.Get(voteID)

	if !exists {
		writeError(w, http.StatusNotFound, "vote_not_found", "vote status not found or expired")
		return
	}

	resp := voteStatusResponse{
		VoteID:    voteID,
		VotingID:  entry.VotingID,
		Status:    string(entry.Status),
		UpdatedAt: entry.UpdatedAt,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (app *application) createPolicy(w http.ResponseWriter, r *http.Request) {
	votingID := r.PathValue("votingId")

	app.votingsMu.RLock()
	_, ok := app.votings[votingID]
	app.votingsMu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "voting_not_found", "voting not found")
		return
	}

	var req policyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}
	if err := validatePolicy(req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	now := time.Now().UTC()
	evt := policyControlEvent{
		PolicyEventID: newULID(),
		VotingID:      votingID,
		TargetType:    req.TargetType,
		TargetValue:   req.TargetValue,
		Action:        req.Action,
		EffectiveMode: req.EffectiveMode,
		Reason:        req.Reason,
		CreatedAt:     now,
	}

	if err := kafkautil.PublishJSON(app.policyProducer, app.cfg.TopicPolicyControl, []byte(votingID), evt); err != nil {
		writeError(w, http.StatusInternalServerError, "kafka_publish_failed", "failed to persist policy")
		return
	}

	app.policyState.SetBlocked(votingID, req.TargetValue, req.Action == "ACTIVATE")

	resp := policyResponse{
		PolicyEventID: evt.PolicyEventID,
		VotingID:      votingID,
		TargetType:    req.TargetType,
		TargetValue:   req.TargetValue,
		Action:        req.Action,
		EffectiveMode: req.EffectiveMode,
		CreatedAt:     now,
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (app *application) handleCatalogLatest(msg *kafka.Message) {
	var v voting
	if err := json.Unmarshal(msg.Value, &v); err != nil {
		app.logger.Error("catalog latest decode error", "error", err)
		return
	}
	v.AntiAbuse = app.normalizeAntiAbuseConfig(&v.AntiAbuse)

	app.votingsMu.Lock()
	app.votings[v.VotingID] = v
	app.votingsMu.Unlock()
}

func (app *application) handlePolicyLatest(msg *kafka.Message) {
	var pol policyLatestEvent
	if err := json.Unmarshal(msg.Value, &pol); err != nil {
		app.logger.Error("policy latest decode error", "error", err)
		return
	}
	app.policyState.SetBlocked(pol.VotingID, pol.TargetValue, pol.Active)
}

func (app *application) handleSnapshot(msg *kafka.Message) {
	var snap resultsSnapshotEvent
	if err := json.Unmarshal(msg.Value, &snap); err != nil {
		app.logger.Error("snapshot decode error", "error", err)
		return
	}

	app.snapshotMu.Lock()
	app.snapshots[snap.VotingID] = domain.PublicResultsFromSnapshot(snap)
	app.snapshotMu.Unlock()
}

var validateVoting = domain.ValidateVoting
var validatePolicy = domain.ValidatePolicy
var hasCandidate = domain.HasCandidate

func newULID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	return ulid.MustNew(ulid.Timestamp(time.Now().UTC()), entropy).String()
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		slog.Default().Error("json encode failed", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}

func resolveClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		return strings.TrimSpace(remoteAddr)
	}
	return host
}

func subtleConstantTimeEquals(got, expected string) bool {
	gotBytes := []byte(got)
	expectedBytes := []byte(expected)
	if len(gotBytes) != len(expectedBytes) {
		return false
	}
	return hmac.Equal(gotBytes, expectedBytes)
}
