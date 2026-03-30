package service

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	configpkg "votingplatform/api/internal/config"

	"github.com/redis/go-redis/v9"
)

type AntiAbuseStore interface {
	MarkChallengeUsed(challengeID string, expiresAt time.Time) (bool, error)
	RecordChallengeIssued(votingID, ip, sessionID string, at time.Time, window time.Duration) error
	RecordVoteAccepted(votingID, ip, sessionID string, at time.Time, window time.Duration) error
	CountRecentVotesByIP(votingID, ip string, cutoff time.Time) (int, error)
	CountRecentChallengesByIP(votingID, ip string, cutoff time.Time) (int, error)
	CountRecentSessionActivity(votingID, ip, sessionID string, cutoff time.Time) (int, error)
	Close() error
}

func NewAntiAbuseStore(cfg configpkg.Config) (AntiAbuseStore, error) {
	if cfg.AntiAbuseStore == "valkey" {
		return NewValkeyAntiAbuseStore(cfg)
	}
	return NewMemoryAntiAbuseStore(), nil
}

func NewMemoryAntiAbuseStore() AntiAbuseStore {
	return &memoryAntiAbuseStore{
		usedChallenges:    make(map[string]time.Time),
		challengeActivity: make(map[string][]time.Time),
		voteActivity:      make(map[string][]time.Time),
		sessionActivity:   make(map[string][]time.Time),
	}
}

type memoryAntiAbuseStore struct {
	mu sync.Mutex

	usedChallenges    map[string]time.Time
	challengeActivity map[string][]time.Time
	voteActivity      map[string][]time.Time
	sessionActivity   map[string][]time.Time
}

func (s *memoryAntiAbuseStore) MarkChallengeUsed(challengeID string, expiresAt time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, expiry := range s.usedChallenges {
		if !expiry.IsZero() && !expiry.After(now) {
			delete(s.usedChallenges, id)
		}
	}
	if expiry, exists := s.usedChallenges[challengeID]; exists && (expiry.IsZero() || expiry.After(now)) {
		return false, nil
	}
	s.usedChallenges[challengeID] = expiresAt.UTC()
	return true, nil
}

func (s *memoryAntiAbuseStore) RecordChallengeIssued(votingID, ip, sessionID string, at time.Time, window time.Duration) error {
	s.record(&s.challengeActivity, ipKey(votingID, ip), at, window)
	s.record(&s.sessionActivity, sessionKey(votingID, ip, sessionID), at, window)
	return nil
}

func (s *memoryAntiAbuseStore) RecordVoteAccepted(votingID, ip, sessionID string, at time.Time, window time.Duration) error {
	s.record(&s.voteActivity, ipKey(votingID, ip), at, window)
	s.record(&s.sessionActivity, sessionKey(votingID, ip, sessionID), at, window)
	return nil
}

func (s *memoryAntiAbuseStore) CountRecentVotesByIP(votingID, ip string, cutoff time.Time) (int, error) {
	return s.count(s.voteActivity, ipKey(votingID, ip), cutoff), nil
}

func (s *memoryAntiAbuseStore) CountRecentChallengesByIP(votingID, ip string, cutoff time.Time) (int, error) {
	return s.count(s.challengeActivity, ipKey(votingID, ip), cutoff), nil
}

func (s *memoryAntiAbuseStore) CountRecentSessionActivity(votingID, ip, sessionID string, cutoff time.Time) (int, error) {
	return s.count(s.sessionActivity, sessionKey(votingID, ip, sessionID), cutoff), nil
}

func (s *memoryAntiAbuseStore) Close() error {
	return nil
}

func (s *memoryAntiAbuseStore) record(store *map[string][]time.Time, key string, at time.Time, window time.Duration) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := at.UTC()
	entries := append((*store)[key], now)
	(*store)[key] = trimEntries(entries, now.Add(-window))
}

func (s *memoryAntiAbuseStore) count(store map[string][]time.Time, key string, cutoff time.Time) int {
	if key == "" {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries := trimEntries(store[key], cutoff.UTC())
	if len(entries) == 0 {
		delete(store, key)
		return 0
	}
	store[key] = entries
	return len(entries)
}

func trimEntries(entries []time.Time, cutoff time.Time) []time.Time {
	if len(entries) == 0 {
		return nil
	}
	kept := entries[:0]
	for _, ts := range entries {
		if !ts.Before(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func NewValkeyAntiAbuseStore(cfg configpkg.Config) (AntiAbuseStore, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.ValkeyAddr,
		Password: cfg.ValkeyPassword,
		DB:       cfg.ValkeyDB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &valkeyAntiAbuseStore{client: client, keyPrefix: cfg.ValkeyKeyPrefix}, nil
}

type valkeyAntiAbuseStore struct {
	client    *redis.Client
	keyPrefix string
}

func (s *valkeyAntiAbuseStore) MarkChallengeUsed(challengeID string, expiresAt time.Time) (bool, error) {
	ttl := time.Until(expiresAt.UTC())
	if ttl <= 0 {
		ttl = time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	created, err := s.client.SetNX(ctx, s.key("pow:used", challengeID), expiresAt.UTC().Format(time.RFC3339Nano), ttl).Result()
	if err != nil {
		return false, err
	}
	return created, nil
}

func (s *valkeyAntiAbuseStore) RecordChallengeIssued(votingID, ip, sessionID string, at time.Time, window time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pipe := s.client.TxPipeline()
	s.addActivity(ctx, pipe, s.key("pow:activity:challenge:ip", ipKey(votingID, ip)), at, window)
	s.addActivity(ctx, pipe, s.key("pow:activity:session", sessionKey(votingID, ip, sessionID)), at, window)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *valkeyAntiAbuseStore) RecordVoteAccepted(votingID, ip, sessionID string, at time.Time, window time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pipe := s.client.TxPipeline()
	s.addActivity(ctx, pipe, s.key("pow:activity:vote:ip", ipKey(votingID, ip)), at, window)
	s.addActivity(ctx, pipe, s.key("pow:activity:session", sessionKey(votingID, ip, sessionID)), at, window)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *valkeyAntiAbuseStore) CountRecentVotesByIP(votingID, ip string, cutoff time.Time) (int, error) {
	return s.countActivity(s.key("pow:activity:vote:ip", ipKey(votingID, ip)), cutoff)
}

func (s *valkeyAntiAbuseStore) CountRecentChallengesByIP(votingID, ip string, cutoff time.Time) (int, error) {
	return s.countActivity(s.key("pow:activity:challenge:ip", ipKey(votingID, ip)), cutoff)
}

func (s *valkeyAntiAbuseStore) CountRecentSessionActivity(votingID, ip, sessionID string, cutoff time.Time) (int, error) {
	return s.countActivity(s.key("pow:activity:session", sessionKey(votingID, ip, sessionID)), cutoff)
}

func (s *valkeyAntiAbuseStore) Close() error {
	return s.client.Close()
}

func (s *valkeyAntiAbuseStore) addActivity(ctx context.Context, pipe redis.Pipeliner, key string, at time.Time, window time.Duration) {
	if key == "" {
		return
	}
	score := float64(at.UTC().UnixMilli())
	member := fmt.Sprintf("%d-%d", at.UTC().UnixNano(), rand.Int63())
	pipe.ZAdd(ctx, key, redis.Z{Score: score, Member: member})
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("(%d", at.UTC().Add(-window).UnixMilli()))
	pipe.Expire(ctx, key, window+time.Minute)
}

func (s *valkeyAntiAbuseStore) countActivity(key string, cutoff time.Time) (int, error) {
	if key == "" {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cutoffMs := cutoff.UTC().UnixMilli()
	pipe := s.client.TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("(%d", cutoffMs))
	countCmd := pipe.ZCount(ctx, key, fmt.Sprintf("%d", cutoffMs), "+inf")
	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	count, err := countCmd.Result()
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *valkeyAntiAbuseStore) key(parts ...string) string {
	out := s.keyPrefix
	for _, part := range parts {
		if part == "" {
			continue
		}
		if out != "" {
			out += ":"
		}
		out += part
	}
	return out
}

func ipKey(votingID, ip string) string {
	if votingID == "" || ip == "" {
		return ""
	}
	return votingID + ":" + ip
}

func sessionKey(votingID, ip, sessionID string) string {
	if votingID == "" || ip == "" || sessionID == "" {
		return ""
	}
	return votingID + ":" + ip + ":" + sessionID
}
