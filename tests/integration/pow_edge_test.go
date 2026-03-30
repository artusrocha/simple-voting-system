package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestPoWEdge(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())
	edge := support.NewAPIClient(support.GetEdgeURL())

	time.Sleep(3500 * time.Millisecond)

	t.Run("create voting via API", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "PoW smoke",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
			"antiAbuse": map[string]interface{}{
				"honeypotEnabled":            true,
				"slideVoteMode":            "full",
				"interactionTelemetryEnabled": true,
				"pow": map[string]interface{}{
					"enabled":               true,
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

		t.Run("open voting via API", func(t *testing.T) {
			patchBody := map[string]string{"status": "OPEN"}
			resp, err := api.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "open voting should return 200")
		})

		t.Run("request PoW challenge through frontend edge", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
			require.NoError(t, err)
			require.Equal(t, 201, resp.StatusCode, "create challenge should return 201")

			challengeID, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
			token, _ := support.JSONPath(resp.BodyMap, "token").(string)

			t.Logf("challengeId: %s", challengeID)

			t.Run("solve PoW challenge", func(t *testing.T) {
				nonce, err := support.SolvePoW(token)
				require.NoError(t, err, "should solve PoW")
				t.Logf("nonce: %s", nonce)

				t.Run("submit vote with valid PoW", func(t *testing.T) {
					voteBody := map[string]interface{}{
						"candidateId": "c1",
						"pow": map[string]string{
							"token": token,
							"nonce": nonce,
						},
					}
					resp, err := edge.Post("/votings/"+votingID+"/votes/"+challengeID, voteBody)
					require.NoError(t, err)
					require.Equal(t, 202, resp.StatusCode, "submit vote should return 202")
				})

				t.Run("wait for edge rate-limit window before reuse check", func(t *testing.T) {
					var lastResp *support.HTTPResponse
					err := support.PollUntil(500*time.Millisecond, 20*time.Second, func() (bool, error) {
						voteBody := map[string]interface{}{
							"candidateId": "c1",
							"pow": map[string]string{
								"token": token,
								"nonce": nonce,
							},
						}
						resp, err := edge.Post("/votings/"+votingID+"/votes/"+challengeID, voteBody)
						if err != nil {
							return false, err
						}
						lastResp = resp
						if resp.StatusCode == 429 {
							t.Logf("rate-limited, waiting... (status=%d)", resp.StatusCode)
							return false, nil
						}
						return true, nil
					})
					require.NoError(t, err, "should exit rate-limit window within timeout")

					require.NotNil(t, lastResp, "should have captured final response")
					require.Equal(t, 403, lastResp.StatusCode, "reuse challenge should return 403: got status=%d body=%s", lastResp.StatusCode, string(lastResp.Body))

					code, _ := support.JSONPath(lastResp.BodyMap, "code").(string)
					require.Equal(t, "pow_reused", code, "expected pow_reused code")
				})
			})
		})
	})

	t.Log("PoW smoke completed successfully")
}
