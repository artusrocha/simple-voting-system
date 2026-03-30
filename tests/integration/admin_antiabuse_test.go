package integration

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"votingplatform/integration/support"
)

func TestAdminAntiAbuse(t *testing.T) {
	api := support.NewAPIClient(support.GetAPIURL())
	frontendURL := support.GetFrontendURL()

	t.Run("check admin page renders anti-abuse fields", func(t *testing.T) {
		resp, err := http.Get(frontendURL + "/admin.html")
		require.NoError(t, err)
		defer resp.Body.Close()

		require.Equal(t, 200, resp.StatusCode, "fetch admin page should return 200")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		content := string(body)
		require.True(t, strings.Contains(content, "antiabuse-honeypot-enabled"), "admin honeypot field should be present")
		require.True(t, strings.Contains(content, "antiabuse-slide-vote-mode"), "admin slide mode field should be present")
		require.True(t, strings.Contains(content, "antiabuse-pow-enabled"), "admin pow enabled field should be present")
		require.True(t, strings.Contains(content, "antiabuse-pow-ttl-seconds"), "admin pow ttl field should be present")
	})

	t.Run("create voting with admin-style antiAbuse payload", func(t *testing.T) {
		body := map[string]interface{}{
			"name":   "Admin anti-abuse smoke",
			"status": "CREATED",
			"candidates": []map[string]string{
				{"candidateId": "c1", "name": "Alice"},
				{"candidateId": "c2", "name": "Bob"},
			},
			"antiAbuse": map[string]interface{}{
				"honeypotEnabled":            true,
				"slideVoteMode":             "button",
				"interactionTelemetryEnabled": true,
				"pow": map[string]interface{}{
					"enabled":               true,
					"ttlSeconds":           45,
					"baseDifficultyBits":   18,
					"maxDifficultyBits":    24,
					"adaptiveWindowSeconds": 120,
				},
			},
		}
		resp, err := api.Post("/votings", body)
		require.NoError(t, err)
		require.Equal(t, 201, resp.StatusCode, "create voting should return 201")

		votingID, ok := support.JSONPath(resp.BodyMap, "votingId").(string)
		require.True(t, ok, "votingId should be present")
		t.Logf("created voting: %s", votingID)

		t.Run("validate created antiAbuse payload", func(t *testing.T) {
			slideMode, _ := support.JSONPath(resp.BodyMap, "antiAbuse.slideVoteMode").(string)
			require.Equal(t, "button", slideMode, "expected created slideVoteMode=button")

			powTTL, _ := support.JSONPath(resp.BodyMap, "antiAbuse.pow.ttlSeconds").(float64)
			require.Equal(t, float64(45), powTTL, "expected created pow ttl=45")

			powEnabled, _ := support.JSONPath(resp.BodyMap, "antiAbuse.pow.enabled").(bool)
			require.True(t, powEnabled, "expected created pow.enabled=true")
		})

		t.Run("patch antiAbuse settings like admin edit form", func(t *testing.T) {
			patchBody := map[string]interface{}{
				"antiAbuse": map[string]interface{}{
					"honeypotEnabled": false,
					"slideVoteMode":  "full",
					"pow": map[string]interface{}{
						"enabled":    false,
						"ttlSeconds": 90,
					},
				},
			}
			patchResp, err := api.Patch("/votings/"+votingID, patchBody)
			require.NoError(t, err)
			require.Equal(t, 200, patchResp.StatusCode, "patch voting should return 200")
		})

		t.Run("validate patched antiAbuse merge result", func(t *testing.T) {
			fetchResp, err := api.Get("/votings/" + votingID)
			require.NoError(t, err)
			require.Equal(t, 200, fetchResp.StatusCode, "get voting should return 200")

			honeypot, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.honeypotEnabled").(bool)
			require.False(t, honeypot, "expected patched honeypotEnabled=false")

			slideMode, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.slideVoteMode").(string)
			require.Equal(t, "full", slideMode, "expected patched slideVoteMode=full")

			powEnabled, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.pow.enabled").(bool)
			require.False(t, powEnabled, "expected patched pow.enabled=false")

			powTTL, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.pow.ttlSeconds").(float64)
			require.Equal(t, float64(90), powTTL, "expected patched pow ttl=90")

			powBase, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.pow.baseDifficultyBits").(float64)
			require.Equal(t, float64(18), powBase, "expected patched pow baseDifficultyBits preserved")

			telemetry, _ := support.JSONPath(fetchResp.BodyMap, "antiAbuse.interactionTelemetryEnabled").(bool)
			require.True(t, telemetry, "expected patched telemetry preserved")
		})
	})

	t.Log("admin anti-abuse smoke completed successfully")
}
