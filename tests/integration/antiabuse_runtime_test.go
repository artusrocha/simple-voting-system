package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestAntiAbuseRuntime(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())
	edge := support.NewAPIClient(support.GetEdgeURL())

	t.Run("create voting with PoW off and honeypot on", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Runtime anti-abuse",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
			"antiAbuse": map[string]interface{}{
				"honeypotEnabled":           true,
				"slideVoteMode":            "off",
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
		})

		t.Run("confirm initial challenge endpoint is disabled", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
			require.NoError(t, err)
			require.Equal(t, 404, resp.StatusCode, "pow disabled challenge should return 404")
		})

		t.Run("patch voting to enable PoW and disable honeypot", func(t *testing.T) {
			patchBody := map[string]interface{}{
				"antiAbuse": map[string]interface{}{
					"honeypotEnabled": false,
					"slideVoteMode":   "full",
					"interactionTelemetryEnabled": true,
					"pow": map[string]interface{}{
						"enabled": true,
					},
				},
			}
			resp, err := api.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "patch anti-abuse should return 200")
		})

		t.Run("fetch voting to validate runtime antiAbuse", func(t *testing.T) {
			resp, err := api.Get("/votings/" + votingID)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "get voting should return 200")

			powEnabled, _ := support.JSONPath(resp.BodyMap, "antiAbuse.pow.enabled").(bool)
			require.True(t, powEnabled, "expected pow.enabled=true after patch")

			honeypotEnabled, _ := support.JSONPath(resp.BodyMap, "antiAbuse.honeypotEnabled").(bool)
			require.False(t, honeypotEnabled, "expected honeypotEnabled=false after patch")

			slideMode, _ := support.JSONPath(resp.BodyMap, "antiAbuse.slideVoteMode").(string)
			require.Equal(t, "full", slideMode, "expected slideVoteMode=full after patch")
		})

		t.Run("request PoW challenge after patch", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
			require.NoError(t, err)
			require.Equal(t, 201, resp.StatusCode, "pow enabled challenge should return 201")

			challengeID, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
			token, _ := support.JSONPath(resp.BodyMap, "token").(string)

			t.Logf("challengeId: %s", challengeID)

			nonce, err := support.SolvePoW(token)
			require.NoError(t, err, "should solve PoW")
			t.Logf("nonce: %s", nonce)

			t.Run("submit vote with honeypot payload and valid PoW", func(t *testing.T) {
				voteBody := map[string]interface{}{
					"candidateId": "c1",
					"honeypot":   "still-filled",
					"pow": map[string]string{
						"token": token,
						"nonce": nonce,
					},
				}
				resp, err := edge.Post("/votings/"+votingID+"/votes/"+challengeID, voteBody)
				require.NoError(t, err)
				require.Equal(t, 202, resp.StatusCode, "vote after anti-abuse patch should return 202")
			})
		})
	})

	t.Log("runtime anti-abuse smoke completed successfully")
}
