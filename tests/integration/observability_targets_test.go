package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

type PrometheusTargets struct {
	Data struct {
		ActiveTargets []struct {
			Labels    map[string]string `json:"labels"`
			Health    string            `json:"health"`
			ScrapeURL string            `json:"scrapeUrl"`
		} `json:"activeTargets"`
	} `json:"data"`
}

func TestObservabilityTargets(t *testing.T) {
	prometheusURL := support.GetPrometheusURL()

	expectedJobs := []string{
		"api",
		"projector",
		"prometheus",
		"node-exporter",
		"kafka-exporter",
	}

	t.Run("query Prometheus active targets", func(t *testing.T) {
		resp, err := http.Get(prometheusURL + "/api/v1/targets")
		require.NoError(t, err)
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var targets PrometheusTargets
		err = json.Unmarshal(body, &targets)
		require.NoError(t, err, "should parse Prometheus response")

		byJob := make(map[string][]struct {
			Health    string
			ScrapeURL string
		})

		for _, target := range targets.Data.ActiveTargets {
			job := target.Labels["job"]
			if job != "" {
				byJob[job] = append(byJob[job], struct {
					Health    string
					ScrapeURL string
				}{target.Health, target.ScrapeURL})
			}
		}

		missing := []string{}
		unhealthy := []string{}

		for _, job := range expectedJobs {
			targets, exists := byJob[job]
			if !exists || len(targets) == 0 {
				missing = append(missing, job)
				continue
			}

			hasHealthy := false
			for _, target := range targets {
				if target.Health == "up" {
					hasHealthy = true
					t.Logf("%s health=%s url=%s", job, target.Health, target.ScrapeURL)
					break
				}
			}
			if !hasHealthy {
				unhealthy = append(unhealthy, job)
			}
		}

		require.Empty(t, missing, "missing jobs: %v", missing)
		require.Empty(t, unhealthy, "jobs without healthy target: %v", unhealthy)
	})

	_ = fmt.Sprintf("%s/api/v1/targets", prometheusURL)

	t.Log("all expected Prometheus targets are healthy")
}
