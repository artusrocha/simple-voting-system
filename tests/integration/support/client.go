package support

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

type HTTPResponse struct {
	StatusCode int
	Body       []byte
	BodyMap    map[string]interface{}
}

type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Cookies    []*http.Cookie
}

func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *APIClient) Request(method, path string, body interface{}) (*HTTPResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range c.Cookies {
		req.AddCookie(cookie)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var bodyMap map[string]interface{}
	json.Unmarshal(respBody, &bodyMap)

	for _, cookie := range resp.Cookies() {
		c.Cookies = append(c.Cookies, cookie)
	}

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		BodyMap:    bodyMap,
	}, nil
}

func (c *APIClient) Get(path string) (*HTTPResponse, error) {
	return c.Request("GET", path, nil)
}

func (c *APIClient) Post(path string, body interface{}) (*HTTPResponse, error) {
	return c.Request("POST", path, body)
}

func (c *APIClient) Patch(path string, body interface{}) (*HTTPResponse, error) {
	return c.Request("PATCH", path, body)
}

func (c *APIClient) Delete(path string) (*HTTPResponse, error) {
	return c.Request("DELETE", path, nil)
}

func (c *APIClient) CopyCookiesFrom(other *APIClient) {
	c.Cookies = append(c.Cookies, other.Cookies...)
}

func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func GetAPIURL() string {
	return GetEnv("API_BASE_URL", "http://localhost:8080")
}

func GetEdgeURL() string {
	return GetEnv("EDGE_BASE_URL", "http://localhost:3000/api")
}

func GetPrometheusURL() string {
	return GetEnv("PROMETHEUS_URL", "http://localhost:19090")
}

func GetProjectorHealthURL() string {
	return GetEnv("PROJECTOR_HEALTH_URL", "http://localhost:8081/healthz")
}

func GetFrontendURL() string {
	return GetEnv("FRONTEND_BASE_URL", "http://localhost:3000")
}

func GetAPIBURL() string {
	return GetEnv("API_B_BASE_URL", "http://localhost:8082")
}

func JSONPath(data map[string]interface{}, path string) interface{} {
	keys := splitPath(path)
	current := interface{}(data)

	for _, key := range keys {
		switch v := current.(type) {
		case map[string]interface{}:
			current = v[key]
		case []interface{}:
			idx := 0
			fmt.Sscanf(key, "%d", &idx)
			if idx < len(v) {
				current = v[idx]
			} else {
				return nil
			}
		default:
			return nil
		}
	}
	return current
}

func splitPath(path string) []string {
	var keys []string
	current := ""
	for _, c := range path {
		if c == '.' {
			if current != "" {
				keys = append(keys, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		keys = append(keys, current)
	}
	return keys
}
