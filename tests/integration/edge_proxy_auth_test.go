package integration

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestEdgeProxyAuth(t *testing.T) {
	if os.Getenv("API_EDGE_PROXY_SHARED_SECRET") == "" {
		t.Skip("Skipping: API_EDGE_PROXY_SHARED_SECRET not set - edge proxy auth not configured")
	}

	api := support.NewAPIClient(support.GetAPIURL())
	edge := support.NewAPIClient(support.GetEdgeURL())

	t.Run("healthz stays available without edge header", func(t *testing.T) {
		resp, err := api.Get("/healthz")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "healthz should return 200")
	})

	t.Run("metrics stays available without edge header", func(t *testing.T) {
		resp, err := api.Get("/metrics")
		require.NoError(t, err)
		require.Equal(t, 200, resp.StatusCode, "metrics should return 200")
	})

	t.Run("direct API access is blocked without edge authentication", func(t *testing.T) {
		resp, err := api.Get("/votings")
		require.NoError(t, err)
		require.Equal(t, 403, resp.StatusCode, "direct API should be blocked with 403")

		code, _ := support.JSONPath(resp.BodyMap, "code").(string)
		require.Equal(t, "edge_proxy_required", code, "expected edge_proxy_required code")
	})

	t.Run("create voting through frontend edge", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Edge auth smoke",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
		}
		resp, err := edge.Post("/votings", body)
		require.NoError(t, err)
		require.Equal(t, 201, resp.StatusCode, "create voting via edge should return 201")

		votingID, ok := support.JSONPath(resp.BodyMap, "votingId").(string)
		require.True(t, ok, "votingId should be present")
		t.Logf("created voting: %s", votingID)

		t.Run("open voting through frontend edge", func(t *testing.T) {
			patchBody := map[string]string{"status": "OPEN"}
			resp, err := edge.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, "open voting via edge should return 200")
		})

		t.Run("submit vote through frontend edge", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.99",
			}
			resp, err := edge.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 202, resp.StatusCode, "vote via edge should return 202")
		})

		t.Run("direct vote submission remains blocked", func(t *testing.T) {
			body := map[string]string{
				"candidateId": "c1",
				"ip":         "203.0.113.99",
			}
			resp, err := api.Post("/votings/"+votingID+"/votes", body)
			require.NoError(t, err)
			require.Equal(t, 403, resp.StatusCode, "direct vote should be blocked with 403")

			code, _ := support.JSONPath(resp.BodyMap, "code").(string)
			require.Equal(t, "edge_proxy_required", code, "expected edge_proxy_required for direct vote")
		})
	})

	t.Log("edge proxy authentication smoke completed successfully")
}
