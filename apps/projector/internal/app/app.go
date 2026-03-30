package app

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	configpkg "votingplatform/projector/internal/config"
	domain "votingplatform/projector/internal/domain"
	httpapi "votingplatform/projector/internal/httpapi"
	"votingplatform/projector/internal/kafkautil"
	"votingplatform/projector/internal/logutil"
	service "votingplatform/projector/internal/service"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type votingCatalogEvent = domain.VotingCatalogEvent
type voteRawEvent = domain.VoteRawEvent
type policyControlEvent = domain.PolicyControlEvent
type policyLatestEvent = domain.PolicyLatestEvent
type resultsSnapshotEvent = domain.ResultsSnapshotEvent

type application struct {
	cfg    configpkg.Config
	logger *slog.Logger

	state       *service.State
	metricsMu   sync.Mutex
	metricIndex map[string]snapshotMetricIndex
	pendingMu   sync.Mutex
	pending     map[string]resultsSnapshotEvent
	dirty       map[string]struct{}

	snapshotProducer *kafka.Producer
	catalogProducer  *kafka.Producer
	policyProducer   *kafka.Producer
}

type snapshotMetricIndex struct {
	byCandidate map[string]struct{}
	byHour      map[string]struct{}
}

var publishJSON = kafkautil.PublishJSON

var (
	projectorEventsConsumed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "projector_events_consumed_total",
		Help: "Total events consumed by projector by topic.",
	}, []string{"topic"})

	projectorSnapshotUpdates = promauto.NewCounter(prometheus.CounterOpts{
		Name: "projector_snapshot_updates_total",
		Help: "Total snapshot updates published.",
	})

	projectorSnapshotFlushes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "projector_snapshot_flush_total",
		Help: "Total snapshot flush cycles executed.",
	})

	projectorSnapshotCoalesced = promauto.NewCounter(prometheus.CounterOpts{
		Name: "projector_snapshot_coalesced_total",
		Help: "Total snapshot updates coalesced before publishing.",
	})

	projectorDirtyVotings = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "projector_dirty_votings",
		Help: "Current number of votings waiting for snapshot flush.",
	})

	projectorSnapshotLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "projector_snapshot_lag_seconds",
		Help: "Seconds between event occurrence and snapshot update time.",
	})

	projectorRebuild = promauto.NewCounter(prometheus.CounterOpts{
		Name: "projector_rebuild_total",
		Help: "Total snapshot recomputations executed.",
	})

	projectorPolicyRetroactive = promauto.NewCounter(prometheus.CounterOpts{
		Name: "projector_policy_retroactive_total",
		Help: "Total retroactive policy events consumed.",
	})

	votingResultsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "voting_results_total_votes",
		Help: "Current materialized total votes by voting.",
	}, []string{"voting_id"})

	votingResultsByCandidate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "voting_results_by_candidate",
		Help: "Current materialized votes by voting and candidate.",
	}, []string{"voting_id", "candidate_id"})

	votingResultsPctByCandidate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "voting_results_percentage_by_candidate",
		Help: "Current materialized vote percentage by voting and candidate.",
	}, []string{"voting_id", "candidate_id"})

	votingResultsByHour = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "voting_results_by_hour",
		Help: "Current materialized votes by voting and hour bucket.",
	}, []string{"voting_id", "hour"})
)

func Run() {
	cfg := configpkg.Load()
	logger := logutil.MustConfigure("projector", cfg.LogLevel, nil)
	app := &application{
		cfg:              cfg,
		logger:           logger,
		state:            service.NewState(),
		metricIndex:      make(map[string]snapshotMetricIndex),
		pending:          make(map[string]resultsSnapshotEvent),
		dirty:            make(map[string]struct{}),
		snapshotProducer: kafkautil.NewProducer(cfg.Brokers),
		catalogProducer:  kafkautil.NewProducer(cfg.Brokers),
		policyProducer:   kafkautil.NewProducer(cfg.Brokers),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	app.bootstrapState(ctx)
	app.startSnapshotFlushLoop(ctx)
	app.consumeTopicFromLatest(ctx, cfg.TopicVotingsCatalog, app.handleVotingsCatalog)
	app.consumeTopicFromLatest(ctx, cfg.TopicPolicyControl, app.handlePolicyControl)
	app.consumeVotesRaw(ctx)

	mux := httpapi.NewMux(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}

	go func() {
		logger.Info("projector listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("projector server failed", "error", err)
			panic(err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	cancel()
	app.flushPendingSnapshots()
	_ = server.Shutdown(shutdownCtx)
	app.snapshotProducer.Flush(5_000)
	app.catalogProducer.Flush(5_000)
	app.policyProducer.Flush(5_000)
	app.snapshotProducer.Close()
	app.catalogProducer.Close()
	app.policyProducer.Close()
}

func (app *application) consumeTopicFromLatest(ctx context.Context, topic string, handler func(context.Context, *kafka.Message)) {
	go func() {
		var consumer *kafka.Consumer
		consecutiveReadErrors := 0
		ensureConsumer := func() *kafka.Consumer {
			if consumer != nil {
				return consumer
			}
			created, err := kafka.NewConsumer(&kafka.ConfigMap{
				"bootstrap.servers": strings.Join(app.cfg.Brokers, ","),
				"group.id":          kafkautil.UniqueGroupID("projector"),
				"auto.offset.reset": "latest",
			})
			if err != nil {
				app.logger.Warn("failed to create consumer", "topic", topic, "error", err)
				time.Sleep(2 * time.Second)
				return nil
			}
			if err := created.Subscribe(topic, nil); err != nil {
				app.logger.Warn("failed to subscribe to topic", "topic", topic, "error", err)
				created.Close()
				time.Sleep(2 * time.Second)
				return nil
			}
			consumer = created
			consecutiveReadErrors = 0
			return consumer
		}
		closeConsumer := func() {
			if consumer == nil {
				return
			}
			consumer.Close()
			consumer = nil
		}
		defer closeConsumer()
		for {
			if ctx.Err() != nil {
				return
			}
			activeConsumer := ensureConsumer()
			if activeConsumer == nil {
				continue
			}

			msg, err := activeConsumer.ReadMessage(500 * time.Millisecond)
			if err != nil {
				if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.IsTimeout() {
					consecutiveReadErrors = 0
					continue
				}
				consecutiveReadErrors++
				app.logger.Warn("consumer read error", "topic", topic, "error", err)
				if shouldRecycleConsumer(err, consecutiveReadErrors) {
					app.logger.Warn("recycling consumer after repeated broker errors", "topic", topic, "consecutiveErrors", consecutiveReadErrors)
					closeConsumer()
					consecutiveReadErrors = 0
				}
				time.Sleep(2 * time.Second)
				continue
			}
			consecutiveReadErrors = 0
			projectorEventsConsumed.WithLabelValues(topic).Inc()
			handler(ctx, msg)
		}
	}()
}

func (app *application) bootstrapState(ctx context.Context) {
	app.bootstrapTopicFromBeginning(ctx, app.cfg.TopicVotingCatalog, app.handleVotingCatalogLatest)
	app.bootstrapTopicFromBeginning(ctx, app.cfg.TopicVotingPolicyLatest, app.handlePolicyLatest)
	app.bootstrapTopicFromBeginning(ctx, app.cfg.TopicResultsSnapshot, app.handleResultsSnapshot)
}

func (app *application) bootstrapTopicFromBeginning(ctx context.Context, topic string, handler func(context.Context, *kafka.Message)) {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    strings.Join(app.cfg.Brokers, ","),
		"group.id":             kafkautil.UniqueGroupID("projector-bootstrap"),
		"enable.partition.eof": true,
	})
	if err != nil {
		app.logger.Warn("failed to create bootstrap consumer", "topic", topic, "error", err)
		return
	}
	defer consumer.Close()

	metadata, err := consumer.GetMetadata(&topic, false, 10_000)
	if err != nil {
		app.logger.Warn("failed to load bootstrap metadata", "topic", topic, "error", err)
		return
	}
	topicMeta, ok := metadata.Topics[topic]
	if !ok || len(topicMeta.Partitions) == 0 {
		return
	}

	assignments := make([]kafka.TopicPartition, 0, len(topicMeta.Partitions))
	for _, partition := range topicMeta.Partitions {
		assignments = append(assignments, kafka.TopicPartition{
			Topic:     &topic,
			Partition: partition.ID,
			Offset:    kafka.OffsetBeginning,
		})
	}
	if err := consumer.Assign(assignments); err != nil {
		app.logger.Warn("failed to assign bootstrap consumer", "topic", topic, "error", err)
		return
	}

	remaining := make(map[int32]struct{}, len(assignments))
	for _, assignment := range assignments {
		remaining[assignment.Partition] = struct{}{}
	}
	deadline := time.Now().Add(15 * time.Second)
	for len(remaining) > 0 {
		select {
		case <-ctx.Done():
			return
		default:
		}
		ev := consumer.Poll(500)
		if ev == nil {
			if time.Now().After(deadline) {
				break
			}
			continue
		}
		switch msg := ev.(type) {
		case *kafka.Message:
			if kafkaErr, ok := msg.TopicPartition.Error.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrPartitionEOF {
				delete(remaining, msg.TopicPartition.Partition)
				continue
			}
			deadline = time.Now().Add(2 * time.Second)
			handler(ctx, msg)
		case kafka.Error:
			app.logger.Warn("bootstrap consumer error", "topic", topic, "error", msg)
		}
	}
}

func (app *application) consumeVotesRaw(ctx context.Context) {
	go func() {
		for {
			if ctx.Err() != nil {
				return
			}
			if err := app.runVotesRawConsumer(ctx); err != nil {
				app.logger.Warn("votes raw consumer stopped", "error", err)
				time.Sleep(2 * time.Second)
				continue
			}
			return
		}
	}()
}

func (app *application) runVotesRawConsumer(ctx context.Context) error {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": strings.Join(app.cfg.Brokers, ","),
		"group.id":          kafkautil.UniqueGroupID("projector-votes"),
	})
	if err != nil {
		return err
	}
	defer consumer.Close()

	offsets, err := app.buildVotesRawStartOffsets(consumer)
	if err != nil {
		return err
	}
	if len(offsets) == 0 {
		return nil
	}

	assignments := make([]kafka.TopicPartition, 0, len(offsets))
	for partition, offset := range offsets {
		part := partition
		assignments = append(assignments, kafka.TopicPartition{Topic: &app.cfg.TopicVotesRaw, Partition: part, Offset: kafka.Offset(offset)})
	}
	if err := consumer.Assign(assignments); err != nil {
		return err
	}

	consecutiveReadErrors := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		msg, err := consumer.ReadMessage(500 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.IsTimeout() {
				consecutiveReadErrors = 0
				continue
			}
			consecutiveReadErrors++
			app.logger.Warn("consumer read error", "topic", app.cfg.TopicVotesRaw, "error", err)
			if shouldRecycleConsumer(err, consecutiveReadErrors) {
				return err
			}
			time.Sleep(2 * time.Second)
			continue
		}
		consecutiveReadErrors = 0
		projectorEventsConsumed.WithLabelValues(app.cfg.TopicVotesRaw).Inc()
		app.handleVotesRaw(ctx, msg)
	}
}

func shouldRecycleConsumer(err error, consecutiveReadErrors int) bool {
	if consecutiveReadErrors < 3 || err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "failed to resolve") || strings.Contains(msg, "brokers are down") || strings.Contains(msg, "all broker connections are down")
}

func (app *application) handleVotingsCatalog(ctx context.Context, msg *kafka.Message) {
	var evt votingCatalogEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		app.logger.Error("votings catalog decode error", "error", err)
		return
	}

	app.state.UpsertVoting(evt.Voting)

	if err := publishJSON(app.catalogProducer, app.cfg.TopicVotingCatalog, []byte(evt.Voting.VotingID), evt.Voting); err != nil {
		app.logger.Error("catalog latest publish error", "votingId", evt.Voting.VotingID, "error", err)
	}

	app.recomputeSnapshot(ctx, evt.Voting.VotingID, evt.EventID)
}

func (app *application) handleVotingCatalogLatest(_ context.Context, msg *kafka.Message) {
	var voting domain.Voting
	if err := json.Unmarshal(msg.Value, &voting); err != nil {
		app.logger.Error("voting catalog latest decode error", "error", err)
		return
	}
	app.state.UpsertVoting(voting)
}

func (app *application) handlePolicyControl(ctx context.Context, msg *kafka.Message) {
	var evt policyControlEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		app.logger.Error("policy control decode error", "error", err)
		return
	}
	if evt.TargetType != "IP" || evt.TargetValue == "" {
		return
	}

	active := evt.Action == "ACTIVATE"
	retroactive := app.state.ApplyPolicy(evt)

	latest := policyLatestEvent{
		VotingID:    evt.VotingID,
		TargetValue: evt.TargetValue,
		Active:      active,
		UpdatedAt:   time.Now().UTC(),
	}
	if err := publishJSON(app.policyProducer, app.cfg.TopicVotingPolicyLatest, []byte(evt.VotingID+"|"+evt.TargetValue), latest); err != nil {
		app.logger.Error("policy latest publish error", "votingId", evt.VotingID, "targetValue", evt.TargetValue, "error", err)
	}

	if retroactive {
		projectorPolicyRetroactive.Inc()
		app.recomputeSnapshot(ctx, evt.VotingID, evt.PolicyEventID)
	}
}

func (app *application) handlePolicyLatest(_ context.Context, msg *kafka.Message) {
	var evt policyLatestEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		app.logger.Error("policy latest decode error", "error", err)
		return
	}
	action := "DEACTIVATE"
	if evt.Active {
		action = "ACTIVATE"
	}
	app.state.ApplyPolicy(domain.PolicyControlEvent{
		VotingID:      evt.VotingID,
		TargetType:    "IP",
		TargetValue:   evt.TargetValue,
		Action:        action,
		EffectiveMode: "FORWARD_ONLY",
	})
}

func (app *application) handleVotesRaw(ctx context.Context, msg *kafka.Message) {
	var evt voteRawEvent
	if err := json.Unmarshal(msg.Value, &evt); err != nil {
		app.logger.Error("vote raw decode error", "error", err)
		return
	}
	if app.state.ShouldSkipVote(evt.VotingID, msg.TopicPartition.Partition, int64(msg.TopicPartition.Offset)) {
		return
	}

	app.applyVoteAndPublish(ctx, evt, msg.TopicPartition.Partition, int64(msg.TopicPartition.Offset))
}

func (app *application) handleResultsSnapshot(_ context.Context, msg *kafka.Message) {
	var snap resultsSnapshotEvent
	if err := json.Unmarshal(msg.Value, &snap); err != nil {
		app.logger.Error("results snapshot decode error", "error", err)
		return
	}
	app.state.SaveSnapshot(snap.VotingID, snap)
	app.recordSnapshotMetrics(snap)
}

func (app *application) applyVoteAndPublish(ctx context.Context, evt voteRawEvent, partition int32, offset int64) {
	snap, ok := service.ApplyVote(app.state, evt, partition, offset)
	if !ok {
		return
	}

	projectorSnapshotLag.Set(time.Since(evt.OccurredAt).Seconds())
	app.recordSnapshotMetrics(snap)
	app.enqueueSnapshotForPublish(snap)
}

func (app *application) recomputeSnapshot(ctx context.Context, votingID, checkpoint string) {
	voting, base, blocked, ok := app.state.RecomputeInputs(votingID)
	if !ok {
		return
	}
	votes, err := app.replayVotesForVoting(ctx, votingID, base.ReplayMetadata)
	if err != nil {
		app.logger.Error("snapshot replay error", "votingId", votingID, "checkpoint", checkpoint, "error", err)
		return
	}
	snap := service.RecomputeSnapshot(voting, base, blocked, votes, checkpoint)
	app.state.SaveSnapshot(votingID, snap)
	app.recordSnapshotMetrics(snap)
	projectorRebuild.Inc()
	app.enqueueSnapshotForPublish(snap)
}

func (app *application) startSnapshotFlushLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(app.cfg.SnapshotFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				app.flushPendingSnapshots()
			}
		}
	}()
}

func (app *application) enqueueSnapshotForPublish(snap resultsSnapshotEvent) {
	app.pendingMu.Lock()
	defer app.pendingMu.Unlock()
	if _, exists := app.dirty[snap.VotingID]; exists {
		projectorSnapshotCoalesced.Inc()
	}
	app.pending[snap.VotingID] = domain.CloneSnapshot(snap)
	app.dirty[snap.VotingID] = struct{}{}
	projectorDirtyVotings.Set(float64(len(app.dirty)))
}

func (app *application) flushPendingSnapshots() {
	app.pendingMu.Lock()
	if len(app.dirty) == 0 {
		app.pendingMu.Unlock()
		return
	}
	snaps := make([]resultsSnapshotEvent, 0, len(app.dirty))
	for votingID := range app.dirty {
		snap, ok := app.pending[votingID]
		if !ok {
			delete(app.dirty, votingID)
			continue
		}
		snaps = append(snaps, snap)
		delete(app.dirty, votingID)
	}
	projectorDirtyVotings.Set(float64(len(app.dirty)))
	app.pendingMu.Unlock()

	projectorSnapshotFlushes.Inc()
	for _, snap := range snaps {
		if err := publishJSON(app.snapshotProducer, app.cfg.TopicResultsSnapshot, []byte(snap.VotingID), snap); err != nil {
			app.logger.Error("snapshot publish error", "votingId", snap.VotingID, "error", err)
			app.pendingMu.Lock()
			app.pending[snap.VotingID] = snap
			app.dirty[snap.VotingID] = struct{}{}
			projectorDirtyVotings.Set(float64(len(app.dirty)))
			app.pendingMu.Unlock()
			continue
		}
		projectorSnapshotUpdates.Inc()
	}
}

func (app *application) buildVotesRawStartOffsets(consumer *kafka.Consumer) (map[int32]int64, error) {
	metadata, err := consumer.GetMetadata(&app.cfg.TopicVotesRaw, false, 10_000)
	if err != nil {
		return nil, err
	}
	topicMeta, ok := metadata.Topics[app.cfg.TopicVotesRaw]
	if !ok {
		return nil, nil
	}

	minOffsets := map[int32]int64{}
	for _, partition := range topicMeta.Partitions {
		minOffsets[partition.ID] = -1
	}

	for _, votingID := range app.state.VotingIDs() {
		snap, ok := app.state.Snapshot(votingID)
		if !ok {
			continue
		}
		for key, offset := range snap.ReplayMetadata.LastProcessedOffsetsByPartition {
			partition, err := strconv.ParseInt(key, 10, 32)
			if err != nil {
				continue
			}
			part := int32(partition)
			offset++
			current, exists := minOffsets[part]
			if !exists || current == -1 || offset < current {
				minOffsets[part] = offset
			}
		}
	}

	startOffsets := make(map[int32]int64, len(topicMeta.Partitions))
	for _, partition := range topicMeta.Partitions {
		part := partition.ID
		if offset, ok := minOffsets[part]; ok && offset >= 0 {
			startOffsets[part] = offset
			continue
		}
		_, high, err := consumer.QueryWatermarkOffsets(app.cfg.TopicVotesRaw, part, 10_000)
		if err != nil {
			return nil, err
		}
		startOffsets[part] = high
	}
	return startOffsets, nil
}

func (app *application) replayVotesForVoting(ctx context.Context, votingID string, meta domain.ReplayMetadata) ([]voteRawEvent, error) {
	if len(meta.InitialOffsetsByPartition) == 0 {
		return nil, nil
	}
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    strings.Join(app.cfg.Brokers, ","),
		"group.id":             kafkautil.UniqueGroupID("projector-replay"),
		"enable.partition.eof": true,
	})
	if err != nil {
		return nil, err
	}
	defer consumer.Close()

	assignments := make([]kafka.TopicPartition, 0, len(meta.InitialOffsetsByPartition))
	endOffsets := make(map[int32]int64, len(meta.InitialOffsetsByPartition))
	completed := make(map[int32]bool, len(meta.InitialOffsetsByPartition))
	for key, start := range meta.InitialOffsetsByPartition {
		partitionValue, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			continue
		}
		partition := int32(partitionValue)
		assignments = append(assignments, kafka.TopicPartition{Topic: &app.cfg.TopicVotesRaw, Partition: partition, Offset: kafka.Offset(start)})
		_, high, err := consumer.QueryWatermarkOffsets(app.cfg.TopicVotesRaw, partition, 10_000)
		if err != nil {
			return nil, err
		}
		endOffsets[partition] = high
		completed[partition] = high <= start
	}
	if len(assignments) == 0 {
		return nil, nil
	}
	if err := consumer.Assign(assignments); err != nil {
		return nil, err
	}

	remaining := 0
	for _, done := range completed {
		if !done {
			remaining++
		}
	}
	votes := make([]voteRawEvent, 0, 256)
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		ev := consumer.Poll(500)
		if ev == nil {
			continue
		}
		switch msg := ev.(type) {
		case *kafka.Message:
			partition := msg.TopicPartition.Partition
			if kafkaErr, ok := msg.TopicPartition.Error.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrPartitionEOF {
				if !completed[partition] {
					completed[partition] = true
					remaining--
				}
				continue
			}
			if int64(msg.TopicPartition.Offset) >= endOffsets[partition] {
				if !completed[partition] {
					completed[partition] = true
					remaining--
				}
				continue
			}
			var vote voteRawEvent
			if err := json.Unmarshal(msg.Value, &vote); err != nil {
				app.logger.Warn("replay vote decode error", "votingId", votingID, "error", err)
				continue
			}
			if vote.VotingID == votingID {
				votes = append(votes, vote)
			}
			if int64(msg.TopicPartition.Offset) >= endOffsets[partition]-1 {
				if !completed[partition] {
					completed[partition] = true
					remaining--
				}
			}
		case kafka.Error:
			return nil, msg
		}
	}
	return votes, nil
}

func (app *application) recordSnapshotMetrics(snap domain.ResultsSnapshotEvent) {
	app.metricsMu.Lock()
	defer app.metricsMu.Unlock()

	index := app.metricIndex[snap.VotingID]
	if index.byCandidate == nil {
		index.byCandidate = make(map[string]struct{})
	}
	if index.byHour == nil {
		index.byHour = make(map[string]struct{})
	}

	votingResultsTotal.WithLabelValues(snap.VotingID).Set(float64(snap.TotalVotes))

	currentCandidates := make(map[string]struct{}, len(snap.ByCandidate))
	for candidateID, count := range snap.ByCandidate {
		currentCandidates[candidateID] = struct{}{}
		votingResultsByCandidate.WithLabelValues(snap.VotingID, candidateID).Set(float64(count))
		votingResultsPctByCandidate.WithLabelValues(snap.VotingID, candidateID).Set(snap.PercentageByCandidate[candidateID])
	}
	for candidateID := range index.byCandidate {
		if _, ok := currentCandidates[candidateID]; ok {
			continue
		}
		votingResultsByCandidate.WithLabelValues(snap.VotingID, candidateID).Set(0)
		votingResultsPctByCandidate.WithLabelValues(snap.VotingID, candidateID).Set(0)
	}
	index.byCandidate = currentCandidates

	currentHours := make(map[string]struct{}, len(snap.ByHour))
	for hour, count := range snap.ByHour {
		currentHours[hour] = struct{}{}
		votingResultsByHour.WithLabelValues(snap.VotingID, hour).Set(float64(count))
	}
	for hour := range index.byHour {
		if _, ok := currentHours[hour]; ok {
			continue
		}
		votingResultsByHour.WithLabelValues(snap.VotingID, hour).Set(0)
	}
	index.byHour = currentHours
	app.metricIndex[snap.VotingID] = index
}
