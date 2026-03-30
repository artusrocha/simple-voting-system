package httpapi

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Handlers struct {
	Healthz             http.HandlerFunc
	CreateVoting        http.HandlerFunc
	ListVotings         http.HandlerFunc
	GetVoting           http.HandlerFunc
	PatchVoting         http.HandlerFunc
	CreateVoteChallenge http.HandlerFunc
	RegisterVote        http.HandlerFunc
	GetResults          http.HandlerFunc
	CreatePolicy        http.HandlerFunc
	GetVoteStatus       http.HandlerFunc
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

var (
	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total HTTP requests by path, method, and status.",
	}, []string{"path", "method", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"path", "method"})
)

func NewMux(h Handlers) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.Healthz)
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("POST /votings", h.CreateVoting)
	mux.HandleFunc("GET /votings", h.ListVotings)
	mux.HandleFunc("GET /votings/{votingId}", h.GetVoting)
	mux.HandleFunc("PATCH /votings/{votingId}", h.PatchVoting)
	mux.HandleFunc("POST /votings/{votingId}/vote-challenges", h.CreateVoteChallenge)
	mux.HandleFunc("POST /votings/{votingId}/votes", h.RegisterVote)
	mux.HandleFunc("POST /votings/{votingId}/votes/{challengeId}", h.RegisterVote)
	mux.HandleFunc("GET /votings/{votingId}/results", h.GetResults)
	mux.HandleFunc("POST /votings/{votingId}/policies", h.CreatePolicy)
	mux.HandleFunc("GET /votes/{voteId}/status", h.GetVoteStatus)
	return mux
}

func Instrument(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		status := fmt.Sprintf("%d", rec.status)
		httpRequestsTotal.WithLabelValues(r.URL.Path, r.Method, status).Inc()
		httpDuration.WithLabelValues(r.URL.Path, r.Method).Observe(time.Since(start).Seconds())
	})
}

func WithCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}
