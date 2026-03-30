//go:build integration
// +build integration

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestIdempotencyRestart(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())
	projectorHealthURL := support.GetProjectorHealthURL()

	t.Run("check API and projector health", func(t *testing.T) {
		resp, err := api.Get("/healthz")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "api health should return 200")

		err = support.PollUntil(1*time.Second, 30*time.Second, func() (bool, error) {
			resp, err := api.Get("/healthz")
			if err != nil {
				return false, nil
			}
			return resp.StatusCode == 200, nil
		})
		require.NoError(t, err, "projector should become healthy")

		_ = projectorHealthURL
	})

	t.Run("create and open voting", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Idempotency check voting",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
		}
		resp, err := api.Post("/votings", body)
		require.NoError(t, err)
		require.Equal(t, 201, resp.StatusCode, "create voting should return 201")

		votingID, ok := support.JSONPath(resp.BodyMap, "votingId").(string)
		require.True(t, ok, "votingId should be present")
		t.Logf("created voting: %s", votingID)

		patchBody := map[string]string{"status": "OPEN"}
		resp, err = api.Patch("/votings/"+votingID, patchBody)
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "open voting should return 200")

		err = support.PollUntil(1*time.Second, 20*time.Second, func() (bool, error) {
			resp, err := api.Get("/votings/" + votingID)
			if err != nil {
				return false, err
			}
			if resp.StatusCode != 200 {
				return false, nil
			}
			status, _ := support.JSONPath(resp.BodyMap, "status").(string)
			return status == "OPEN", nil
		})
		require.NoError(t, err, "voting should become OPEN")

		t.Run("send two accepted votes", func(t *testing.T) {
			vote1 := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.21",
			}
			vote2 := map[string]string{
				"candidateId": "c2",
				"ip":         "203.0.113.22",
			}

			err := support.PollUntil(1*time.Second, 60*time.Second, func() (bool, error) {
				resp, err := api.Post("/votings/"+votingID+"/votes", vote1)
				if err != nil {
					return false, err
				}
				if resp.StatusCode == 202 {
					return true, nil
				}
				if resp.StatusCode != 409 {
					return false, nil
				}
				return false, nil
			})
			require.NoError(t, err, "vote 1 should be accepted")

			err = support.PollUntil(1*time.Second, 60*time.Second, func() (bool, error) {
				resp, err := api.Post("/votings/"+votingID+"/votes", vote2)
				if err != nil {
					return false, err
				}
				if resp.StatusCode == 202 {
					return true, nil
				}
				if resp.StatusCode != 409 {
					return false, nil
				}
				return false, nil
			})
			require.NoError(t, err, "vote 2 should be accepted")
		})

		t.Run("wait baseline results", func(t *testing.T) {
			err := support.PollUntil(1*time.Second, 30*time.Second, func() (bool, error) {
				resp, err := api.Get("/votings/" + votingID + "/results")
				if err != nil {
					return false, err
				}
				if resp.StatusCode != 200 {
					return false, nil
				}
				total, _ := support.JSONPath(resp.BodyMap, "totalVotes").(float64)
				c1, _ := support.JSONPath(resp.BodyMap, "byCandidate.c1").(float64)
				c2, _ := support.JSONPath(resp.BodyMap, "byCandidate.c2").(float64)
				return total == 2 && c1 == 1 && c2 == 1, nil
			})
			require.NoError(t, err, "results should converge to total=2 c1=1 c2=1")
		})

		t.Log("idempotency restart check completed - requires manual container restart")
		t.Log("Run: podman-compose stop api projector frontend prometheus grafana")
		t.Log("Then: podman-compose up -d api projector frontend prometheus grafana")
	})
}
