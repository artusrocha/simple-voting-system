package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"votingplatform/integration/support"
)

func TestVotingE2E(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())

	t.Run("health check", func(t *testing.T) {
		resp, err := api.Get("/healthz")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "healthz should return 200")
	})

	t.Run("create voting", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Smoke voting",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
			"antiAbuse": map[string]interface{}{
				"honeypotEnabled":           true,
				"slideVoteMode":              "off",
				"interactionTelemetryEnabled": false,
				"pow": map[string]interface{}{
					"enabled":               false,
					"ttlSeconds":            60,
					"baseDifficultyBits":    18,
					"maxDifficultyBits":     24,
					"adaptiveWindowSeconds": 60,
				},
			},
		}
		resp, err := api.Post("/votings", body)
		require.NoError(t, err)
		require.Equal(t, 201, resp.StatusCode, "create voting should return 201")

		votingID, ok := support.JSONPath(resp.BodyMap, "votingId").(string)
		require.True(t, ok, "votingId should be present")
		t.Logf("created voting: %s", votingID)

		t.Run("open voting", func(t *testing.T) {
			patchBody := map[string]string{"status": "OPEN"}
			resp, err := api.Patch("/votings/"+votingID, patchBody)
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
		})

		t.Run("submit accepted vote from ip-1", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.10",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 202, resp.StatusCode, "vote should be accepted with 202")
		})

		t.Run("wait for first vote snapshot", func(t *testing.T) {
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
				return total == 1 && c1 == 1, nil
			})
			require.NoError(t, err, "results should converge to total=1 c1=1")
		})

		t.Run("activate forward-only policy for ip-1", func(t *testing.T) {
			body := map[string]interface{}{
				"targetType":    "IP",
				"targetValue":  "203.0.113.10",
				"action":       "ACTIVATE",
				"effectiveMode": "FORWARD_ONLY",
			}
			resp, err := api.Post("/votings/"+votingID+"/policies", body)
			require.NoError(t, err)
			require.Equal(t, 201, resp.StatusCode, "forward policy should return 201")
		})

		t.Run("verify vote from ip-1 is blocked", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.10",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 403, resp.StatusCode, "blocked vote should return 403")
		})

		t.Run("send accepted vote from ip-2", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c2",
				"ip":         "203.0.113.11",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 202, resp.StatusCode, "vote from ip-2 should be accepted")
		})

		t.Run("trigger honeypot protection for ip-3", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.12",
				"honeypot":   "filled-by-bot",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 403, resp.StatusCode, "honeypot vote should be blocked")
		})

		t.Run("verify honeypot ip-3 is temporarily blocked", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.12",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 403, resp.StatusCode, "honeypot blocked ip should return 403")
		})

		t.Run("wait for two-vote snapshot", func(t *testing.T) {
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

		t.Run("activate retroactive policy for ip-2", func(t *testing.T) {
			body := map[string]interface{}{
				"targetType":    "IP",
				"targetValue":  "203.0.113.11",
				"action":       "ACTIVATE",
				"effectiveMode": "RETROACTIVE",
			}
			resp, err := api.Post("/votings/"+votingID+"/policies", body)
			require.NoError(t, err)
			require.Equal(t, 201, resp.StatusCode, "retroactive policy should return 201")
		})

		t.Run("wait for recomputed snapshot after retroactive block", func(t *testing.T) {
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
				return total == 1 && c1 == 1 && c2 == 0, nil
			})
			require.NoError(t, err, "results should converge to total=1 c1=1 c2=0 after retroactive block")
		})
	})

	t.Log("smoke flow completed successfully")
}
