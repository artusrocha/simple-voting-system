package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr              string
	Brokers               []string
	LogLevel              string
	SnapshotFlushInterval time.Duration

	TopicVotesRaw           string
	TopicVotingsCatalog     string
	TopicPolicyControl      string
	TopicResultsSnapshot    string
	TopicVotingCatalog      string
	TopicVotingPolicyLatest string
}

func Load() Config {
	brokers := strings.Split(getEnv("KAFKA_BROKERS", "localhost:9092"), ",")
	for i := range brokers {
		brokers[i] = strings.TrimSpace(brokers[i])
	}

	return Config{
		HTTPAddr:                getEnv("HTTP_ADDR", ":8081"),
		Brokers:                 brokers,
		LogLevel:                getEnv("LOG_LEVEL", "info"),
		SnapshotFlushInterval:   time.Duration(getEnvInt("PROJECTOR_SNAPSHOT_FLUSH_INTERVAL_MS", 333)) * time.Millisecond,
		TopicVotesRaw:           getEnv("KAFKA_TOPIC_VOTES_RAW", "votes.raw"),
		TopicVotingsCatalog:     getEnv("KAFKA_TOPIC_VOTINGS_CATALOG", "votings.catalog"),
		TopicPolicyControl:      getEnv("KAFKA_TOPIC_POLICY_CONTROL", "voting-policy-control"),
		TopicResultsSnapshot:    getEnv("KAFKA_TOPIC_RESULTS_SNAPSHOT", "voting-results-snapshot"),
		TopicVotingCatalog:      getEnv("KAFKA_TOPIC_VOTING_CATALOG_LATEST", "voting-catalog-latest"),
		TopicVotingPolicyLatest: getEnv("KAFKA_TOPIC_VOTING_POLICY_LATEST", "voting-policy-latest"),
	}
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			return parsed
		}
	}
	return fallback
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
