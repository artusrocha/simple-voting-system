package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr string
	Brokers  []string
	LogLevel string

	FeaturePowVote           bool
	PowSecret                string
	PowTTLSeconds            int
	PowAlgorithm             string
	PowBaseDifficultyBits    int
	PowMaxDifficultyBits     int
	PowAdaptiveWindowSeconds int
	PowArgon2MemoryKiB       int
	PowArgon2TimeCost        int
	PowArgon2Parallelism     int
	PowArgon2HashLength      int
	PowTargetMs              int
	PowSessionCookieName     string
	AntiAbuseStore           string
	ValkeyAddr               string
	ValkeyPassword           string
	ValkeyDB                 int
	ValkeyKeyPrefix          string
	EdgeProxySharedSecret    string
	EdgeProxyAuthHeader      string

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
		HTTPAddr:                 getEnv("HTTP_ADDR", ":8080"),
		Brokers:                  brokers,
		LogLevel:                 getEnv("LOG_LEVEL", "info"),
		FeaturePowVote:           getEnvBool("FEATURE_POW_VOTE", false),
		PowSecret:                getEnv("POW_SECRET", "dev-pow-secret"),
		PowTTLSeconds:            getEnvInt("POW_TTL_SECONDS", 20),
		PowAlgorithm:             strings.ToLower(getEnv("POW_ALGORITHM", "sha256")),
		PowBaseDifficultyBits:    getEnvInt("POW_BASE_DIFFICULTY_BITS", 22),
		PowMaxDifficultyBits:     getEnvInt("POW_MAX_DIFFICULTY_BITS", 28),
		PowAdaptiveWindowSeconds: getEnvInt("POW_ADAPTIVE_WINDOW_SECONDS", 60),
		PowArgon2MemoryKiB:       getEnvInt("POW_ARGON2_MEMORY_KIB", 8192),
		PowArgon2TimeCost:        getEnvInt("POW_ARGON2_TIME_COST", 1),
		PowArgon2Parallelism:     getEnvInt("POW_ARGON2_PARALLELISM", 1),
		PowArgon2HashLength:      getEnvInt("POW_ARGON2_HASH_LENGTH", 32),
		PowTargetMs:              getEnvInt("POW_TARGET_MS", 3000),
		PowSessionCookieName:     getEnv("POW_SESSION_COOKIE_NAME", "pow_session"),
		AntiAbuseStore:           strings.ToLower(getEnv("ANTIABUSE_STORE", "memory")),
		ValkeyAddr:               getEnv("VALKEY_ADDR", "localhost:6379"),
		ValkeyPassword:           getEnv("VALKEY_PASSWORD", ""),
		ValkeyDB:                 getEnvInt("VALKEY_DB", 0),
		ValkeyKeyPrefix:          getEnv("VALKEY_KEY_PREFIX", "voting-platform"),
		EdgeProxySharedSecret:    getEnv("EDGE_PROXY_SHARED_SECRET", ""),
		EdgeProxyAuthHeader:      getEnv("EDGE_PROXY_AUTH_HEADER", "X-App-Edge-Auth"),
		TopicVotesRaw:            getEnv("KAFKA_TOPIC_VOTES_RAW", "votes.raw"),
		TopicVotingsCatalog:      getEnv("KAFKA_TOPIC_VOTINGS_CATALOG", "votings.catalog"),
		TopicPolicyControl:       getEnv("KAFKA_TOPIC_POLICY_CONTROL", "voting-policy-control"),
		TopicResultsSnapshot:     getEnv("KAFKA_TOPIC_RESULTS_SNAPSHOT", "voting-results-snapshot"),
		TopicVotingCatalog:       getEnv("KAFKA_TOPIC_VOTING_CATALOG_LATEST", "voting-catalog-latest"),
		TopicVotingPolicyLatest:  getEnv("KAFKA_TOPIC_VOTING_POLICY_LATEST", "voting-policy-latest"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return parsed
}
