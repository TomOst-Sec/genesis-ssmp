package mcpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Middleware Tests ---

func TestAuthMiddleware_ValidBearer(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	handler := authMiddleware("test-secret", inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "ok", rr.Body.String())
}

func TestAuthMiddleware_InvalidBearer(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := authMiddleware("test-secret", inner)

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid token")
}

func TestAuthMiddleware_MissingBearer(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})
	handler := authMiddleware("test-secret", inner)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Body.String(), "missing Bearer")
}

func TestCORSMiddleware_Headers(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware([]string{"https://claude.ai"}, inner)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "https://claude.ai", rr.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rr.Header().Get("Access-Control-Allow-Methods"), "POST")
	assert.Contains(t, rr.Header().Get("Access-Control-Allow-Headers"), "Authorization")
}

func TestCORSMiddleware_Preflight(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("OPTIONS should not reach inner handler")
	})
	handler := corsMiddleware(nil, inner)

	req := httptest.NewRequest("OPTIONS", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
	assert.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestRateLimitMiddleware(t *testing.T) {
	callCount := 0
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})
	handler := rateLimitMiddleware(3, inner) // 3 requests/minute

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code, "request %d should succeed", i)
	}

	// 4th request should be rate limited
	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusTooManyRequests, rr.Code)
	assert.Equal(t, 3, callCount)
}

func TestLoggingMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := loggingMiddleware(inner)

	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

// --- Config Validation Tests ---

func TestConfigValidation(t *testing.T) {
	t.Run("valid none auth", func(t *testing.T) {
		cfg := DefaultConfig()
		assert.NoError(t, cfg.Validate())
	})

	t.Run("valid bearer auth", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.AuthMode = "bearer"
		cfg.BearerToken = "secret"
		assert.NoError(t, cfg.Validate())
	})

	t.Run("bearer without token", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.AuthMode = "bearer"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "--bearer-token")
	})

	t.Run("unknown auth mode", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.AuthMode = "oauth"
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown auth mode")
	})

	t.Run("empty addr", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Addr = ""
		assert.Error(t, cfg.Validate())
	})

	t.Run("empty heaven addr", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.HeavenAddr = ""
		assert.Error(t, cfg.Validate())
	})
}

// --- Tool Tests ---

func TestSSEEndpoint(t *testing.T) {
	mockHeaven := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"state_rev":1,"leases":[],"clocks":{}}`)
	}))
	defer mockHeaven.Close()

	cfg := DefaultConfig()
	cfg.HeavenAddr = strings.TrimPrefix(mockHeaven.URL, "http://")

	handler, sseSrv, err := New(cfg)
	require.NoError(t, err)
	defer sseSrv.Shutdown(nil)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Connect SSE — should get event stream with endpoint event
	sseResp, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	defer sseResp.Body.Close()

	assert.Equal(t, http.StatusOK, sseResp.StatusCode)
	assert.Equal(t, "text/event-stream", sseResp.Header.Get("Content-Type"))

	// Read the endpoint event
	buf := make([]byte, 4096)
	n, err := sseResp.Body.Read(buf)
	require.NoError(t, err)
	eventData := string(buf[:n])
	assert.Contains(t, eventData, "event: endpoint")
	assert.Contains(t, eventData, "/message?sessionId=")
}

func TestMCPProtocol_InitAndListTools(t *testing.T) {
	mockHeaven := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"state_rev":1,"leases":[],"clocks":{}}`)
	}))
	defer mockHeaven.Close()

	cfg := DefaultConfig()
	cfg.HeavenAddr = strings.TrimPrefix(mockHeaven.URL, "http://")

	handler, sseSrv, err := New(cfg)
	require.NoError(t, err)
	defer sseSrv.Shutdown(nil)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Connect SSE
	sseResp, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	defer sseResp.Body.Close()

	// Extract message endpoint
	buf := make([]byte, 4096)
	n, err := sseResp.Body.Read(buf)
	require.NoError(t, err)
	eventData := string(buf[:n])

	var messageURL string
	for _, line := range strings.Split(eventData, "\n") {
		if strings.HasPrefix(line, "data: ") {
			messageURL = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
			break
		}
	}
	require.NotEmpty(t, messageURL)

	// Fix URL to point to test server
	messageURL = strings.Replace(messageURL, fmt.Sprintf("http://%s", cfg.Addr), ts.URL, 1)

	// Initialize — SSE protocol returns 202 (response comes via SSE stream)
	initReq := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}`
	initResp, err := http.Post(messageURL, "application/json", strings.NewReader(initReq))
	require.NoError(t, err)
	initResp.Body.Close()
	assert.Equal(t, http.StatusAccepted, initResp.StatusCode)

	// Read the initialize response from SSE stream
	n, err = sseResp.Body.Read(buf)
	require.NoError(t, err)
	sseData := string(buf[:n])
	assert.Contains(t, sseData, "genesis-mcp")

	// Send initialized notification
	notifReq := `{"jsonrpc":"2.0","method":"notifications/initialized"}`
	notifResp, err := http.Post(messageURL, "application/json", strings.NewReader(notifReq))
	require.NoError(t, err)
	notifResp.Body.Close()

	// List tools
	listReq := `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`
	listResp, err := http.Post(messageURL, "application/json", strings.NewReader(listReq))
	require.NoError(t, err)
	listResp.Body.Close()
	assert.Equal(t, http.StatusAccepted, listResp.StatusCode)

	// Read tools list from SSE stream
	n, err = sseResp.Body.Read(buf)
	require.NoError(t, err)
	toolsData := string(buf[:n])

	// Extract JSON from SSE event format: "event: message\ndata: {...}\n\n"
	for _, line := range strings.Split(toolsData, "\n") {
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimPrefix(line, "data: ")
			var result map[string]interface{}
			err = json.Unmarshal([]byte(jsonData), &result)
			if err == nil {
				if res, ok := result["result"].(map[string]interface{}); ok {
					if tools, ok := res["tools"].([]interface{}); ok {
						assert.Equal(t, ToolCount, len(tools), "should have %d tools", ToolCount)

						// Verify genesis.ping is present
						toolNames := make([]string, len(tools))
						for i, tool := range tools {
							if m, ok := tool.(map[string]interface{}); ok {
								toolNames[i] = m["name"].(string)
							}
						}
						assert.Contains(t, toolNames, "genesis.ping")
						assert.Contains(t, toolNames, "genesis.status")
						assert.Contains(t, toolNames, "genesis.ir_search")
					}
				}
			}
			break
		}
	}
}

func TestStatusTool_MockHeaven(t *testing.T) {
	mockHeaven := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/status" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"state_rev":42,"leases":[],"clocks":{"main.go":5}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer mockHeaven.Close()

	cfg := DefaultConfig()
	cfg.HeavenAddr = strings.TrimPrefix(mockHeaven.URL, "http://")

	handler, sseSrv, err := New(cfg)
	require.NoError(t, err)
	defer sseSrv.Shutdown(nil)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Verify the test server is reachable
	resp, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestServerNew_InvalidConfig(t *testing.T) {
	cfg := Config{} // empty, should fail validation
	_, _, err := New(cfg)
	assert.Error(t, err)
}

func TestServerNew_BearerAuth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AuthMode = "bearer"
	cfg.BearerToken = "my-token"

	handler, sseSrv, err := New(cfg)
	require.NoError(t, err)
	defer sseSrv.Shutdown(nil)

	ts := httptest.NewServer(handler)
	defer ts.Close()

	// Without token — should get 401
	resp, err := http.Get(ts.URL + "/sse")
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	// With token — should get 200
	req, _ := http.NewRequest("GET", ts.URL+"/sse", nil)
	req.Header.Set("Authorization", "Bearer my-token")
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	resp2.Body.Close()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
