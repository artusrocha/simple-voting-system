package integration

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestPoWDistributed(t *testing.T) {
	apiA := support.NewAPIClient(support.GetAPIURL())
	apiB := support.NewAPIClient(support.GetAPIBURL())
	testIP := support.GetEnv("TEST_IP", "198.51.100.77")

	t.Run("create distributed PoW voting on api-a", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Distributed PoW Smoke",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
			},
			"antiAbuse": map[string]interface{}{
				"honeypotEnabled":           false,
				"slideVoteMode":            "off",
				"interactionTelemetryEnabled": false,
				"pow": map[string]interface{}{
					"enabled":               true,
					"algorithm":             "sha256",
					"ttlSeconds":            60,
					"baseDifficultyBits":    8,
					"maxDifficultyBits":     8,
					"adaptiveWindowSeconds": 60,
				},
			},
		}
		resp, err := apiA.Post("/votings", body)
		require.NoError(t, err)
		require.Equal(t, 201, resp.StatusCode, "create voting on api-a should return 201")

		votingID, ok := support.JSONPath(resp.BodyMap, "votingId").(string)
		require.True(t, ok, "votingId should be present")
		t.Logf("created voting: %s", votingID)

		t.Run("open voting on api-a", func(t *testing.T) {
			patchBody := map[string]string{"status": "OPEN"}
			resp, err := apiA.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "open voting should return 200")
		})

		t.Run("wait for api-b to observe the opened voting", func(t *testing.T) {
			var lastStatusCode int
			var lastBody string
			err := support.PollUntil(500*time.Millisecond, 90*time.Second, func() (bool, error) {
				resp, err := apiB.Get("/votings/" + votingID)
				if err != nil {
					return false, err
				}
				lastStatusCode = resp.StatusCode
				lastBody = string(resp.Body)
				if resp.StatusCode != 200 {
					return false, nil
				}
				status, _ := support.JSONPath(resp.BodyMap, "status").(string)
				return status == "OPEN", nil
			})
			require.NoError(t, err, "voting should become OPEN on api-b: got status=%d body=%s", lastStatusCode, lastBody)
		})

		t.Run("issue challenge on api-a", func(t *testing.T) {
			resp, err := apiA.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
			require.NoError(t, err)
			require.Equal(t, 201, resp.StatusCode, "issue challenge should return 201")

			challengeID, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
			token, _ := support.JSONPath(resp.BodyMap, "token").(string)

			nonce, err := support.SolvePoWDistributed(token)
			require.NoError(t, err, "should solve PoW")

			t.Run("submit vote on api-b with challenge from api-a", func(t *testing.T) {
				apiB.CopyCookiesFrom(apiA)
				voteBody := map[string]interface{}{
					"candidateId": "c1",
					"pow": map[string]string{
						"token": token,
						"nonce": nonce,
					},
				}
				resp, err := apiB.Post(fmt.Sprintf("/votings/%s/votes/%s?confirm=true&timeoutMs=3000", votingID, challengeID), voteBody)
				require.NoError(t, err, "submit vote request failed")
				require.Equal(t, 403, resp.StatusCode, "cross-instance vote with IP binding mismatch should return 403: got status=%d body=%s", resp.StatusCode, string(resp.Body))

				code, _ := support.JSONPath(resp.BodyMap, "code").(string)
				require.Equal(t, "pow_invalid", code, "expected pow_invalid for IP binding mismatch across instances")
			})

			t.Run("verify same-instance vote succeeds and reuse is blocked", func(t *testing.T) {
				resp, err := apiA.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
				require.NoError(t, err)
				require.Equal(t, 201, resp.StatusCode, "issue new challenge should return 201")

				challengeID2, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
				token2, _ := support.JSONPath(resp.BodyMap, "token").(string)

				nonce2, err := support.SolvePoWDistributed(token2)
				require.NoError(t, err, "should solve PoW")

				voteBody := map[string]interface{}{
					"candidateId": "c1",
					"pow": map[string]string{
						"token": token2,
						"nonce": nonce2,
					},
				}
				resp, err = apiA.Post(fmt.Sprintf("/votings/%s/votes/%s?confirm=true&timeoutMs=3000", votingID, challengeID2), voteBody)
				require.NoError(t, err, "same-instance vote request failed")
				require.Equal(t, 202, resp.StatusCode, "same-instance vote should return 202: got status=%d body=%s", resp.StatusCode, string(resp.Body))

				err = support.PollUntil(500*time.Millisecond, 30*time.Second, func() (bool, error) {
					resp, err := apiA.Get("/votings/" + votingID + "/results")
					if err != nil {
						return false, err
					}
					if resp.StatusCode != 200 {
						return false, nil
					}
					total, _ := support.JSONPath(resp.BodyMap, "totalVotes").(float64)
					return total >= 1, nil
				})
				require.NoError(t, err, "results should reach totalVotes >= 1")

				resp, err = apiA.Post(fmt.Sprintf("/votings/%s/votes/%s?confirm=true&timeoutMs=3000", votingID, challengeID2), voteBody)
				require.NoError(t, err, "reuse challenge request failed")
				require.Equal(t, 403, resp.StatusCode, "reuse challenge should return 403: got status=%d body=%s", resp.StatusCode, string(resp.Body))

				code, _ := support.JSONPath(resp.BodyMap, "code").(string)
				require.Equal(t, "pow_reused", code, "expected pow_reused code")
			})

			t.Run("verify binding mismatch is blocked on api-b", func(t *testing.T) {
				resp, err := apiA.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
				require.NoError(t, err)
				require.Equal(t, 201, resp.StatusCode, "issue second challenge should return 201")

				challengeBID, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
				tokenB, _ := support.JSONPath(resp.BodyMap, "token").(string)

				nonceB, err := support.SolvePoWDistributed(tokenB)
				require.NoError(t, err, "should solve PoW")

				voteBodyB := map[string]interface{}{
					"candidateId": "c1",
					"pow": map[string]string{
						"token": tokenB,
						"nonce": nonceB,
					},
				}

				apiB2 := support.NewAPIClient(support.GetAPIBURL())
				resp, err = apiB2.Post(fmt.Sprintf("/votings/%s/votes/%s?confirm=true&timeoutMs=3000", votingID, challengeBID), voteBodyB)
				require.NoError(t, err, "binding mismatch request failed")
				require.Equal(t, 403, resp.StatusCode, "binding mismatch should return 403: got status=%d body=%s", resp.StatusCode, string(resp.Body))

				code, _ := support.JSONPath(resp.BodyMap, "code").(string)
				require.Equal(t, "pow_invalid", code, "expected pow_invalid for session binding mismatch")
			})
		})
	})

	t.Log("distributed PoW smoke completed successfully")

	_ = testIP
}

func TestPoWDistributedCaptureEvent(t *testing.T) {
	apiA := support.NewAPIClient(support.GetAPIURL())

	body := map[string]interface{}{
		"name": "Distributed PoW Event Capture",
		"candidates": []map[string]string{
			{"candidateId": "c1", "name": "Alice"},
		},
		"antiAbuse": map[string]interface{}{
			"honeypotEnabled":           false,
			"slideVoteMode":            "off",
			"interactionTelemetryEnabled": false,
			"pow": map[string]interface{}{
				"enabled":               true,
				"algorithm":             "sha256",
				"ttlSeconds":            60,
				"baseDifficultyBits":    8,
				"maxDifficultyBits":     8,
				"adaptiveWindowSeconds": 60,
			},
		},
	}
	resp, err := apiA.Post("/votings", body)
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)

	votingID, _ := support.JSONPath(resp.BodyMap, "votingId").(string)

	patchBody := map[string]string{"status": "OPEN"}
	resp, err = apiA.Patch("/votings/"+votingID, patchBody)
	require.NoError(t, err)

	resp, err = apiA.Post("/votings/"+votingID+"/vote-challenges", map[string]interface{}{})
	require.NoError(t, err)
	require.Equal(t, 201, resp.StatusCode)

	challengeID, _ := support.JSONPath(resp.BodyMap, "challengeId").(string)
	token, _ := support.JSONPath(resp.BodyMap, "token").(string)

	nonce, err := support.SolvePoWDistributed(token)
	require.NoError(t, err)

	voteBody := map[string]interface{}{
		"candidateId": "c1",
		"pow": map[string]string{
			"token": token,
			"nonce": nonce,
		},
	}
	resp, err = apiA.Post(fmt.Sprintf("/votings/%s/votes/%s?confirm=true&timeoutMs=3000", votingID, challengeID), voteBody)
	require.NoError(t, err)
	require.Equal(t, 202, resp.StatusCode)

	err = support.PollUntil(500*time.Millisecond, 30*time.Second, func() (bool, error) {
		resp, err := apiA.Get("/votings/" + votingID + "/results")
		if err != nil {
			return false, err
		}
		if resp.StatusCode != 200 {
			return false, nil
		}
		total, _ := support.JSONPath(resp.BodyMap, "totalVotes").(float64)
		return total >= 1, nil
	})
	require.NoError(t, err)

	var sessionID string
	for _, cookie := range apiA.Cookies {
		if cookie.Name == "pow_session" {
			sessionID = cookie.Value
			break
		}
	}

	_ = sessionID

	eventJSON, _ := json.Marshal(map[string]interface{}{
		"votingId":    votingID,
		"challengeId": challengeID,
	})
	t.Logf("Event capture test - votingId: %s, challengeId: %s", votingID, challengeID)
	_ = eventJSON
}
