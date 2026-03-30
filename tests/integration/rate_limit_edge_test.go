package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestRateLimitEdge(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())
	edge := support.NewAPIClient(support.GetEdgeURL())

	time.Sleep(3500 * time.Millisecond)

	t.Run("create voting via API", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Rate limit smoke",
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

		t.Run("open voting via API", func(t *testing.T) {
			patchBody := map[string]string{"status": "OPEN"}
			resp, err := api.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "open voting should return 200")
		})

		votePayload := map[string]string{
			"candidateId": "c1",
			"ip":         "203.0.113.90",
		}

		t.Run("send first vote through frontend edge", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/votes", votePayload)
			require.NoError(t, err)
			require.Equal(t, 202, resp.StatusCode, "first edge vote should return 202")
		})

		t.Run("send second immediate vote through frontend edge", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/votes", votePayload)
			require.NoError(t, err)
			require.Equal(t, 429, resp.StatusCode, "second edge vote should return 429")

			code, _ := support.JSONPath(resp.BodyMap, "code").(string)
			require.Equal(t, "rate_limited", code, "expected rate_limited code")
		})

		t.Run("wait for rate-limit window to expire", func(t *testing.T) {
			t.Log("waiting 3 seconds for rate-limit cooldown...")
			time.Sleep(3 * time.Second)
		})

		t.Run("send third vote after cooldown", func(t *testing.T) {
			resp, err := edge.Post("/votings/"+votingID+"/votes", votePayload)
			require.NoError(t, err)
			require.Equal(t, 202, resp.StatusCode, "third edge vote after cooldown should return 202")
		})
	})

	t.Log("edge rate-limit smoke completed successfully")
}
