package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestValidateAPIKey_NoEndpoint(t *testing.T) {
	result := ValidateAPIKey(context.Background(), MethodConfig{}, "sk-test")
	assert.True(t, result.Valid)
	assert.Contains(t, result.Detail, "no validation endpoint")
}

func TestValidateAPIKey_200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer sk-test-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":[]}`))
	}))
	defer server.Close()

	cfg := MethodConfig{ValidationURL: server.URL}
	result := ValidateAPIKey(context.Background(), cfg, "sk-test-key")
	assert.True(t, result.Valid)
	assert.Nil(t, result.Error)
}

func TestValidateAPIKey_401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":"invalid_api_key"}`))
	}))
	defer server.Close()

	cfg := MethodConfig{ValidationURL: server.URL}
	result := ValidateAPIKey(context.Background(), cfg, "bad-key")
	assert.False(t, result.Valid)
	assert.NotNil(t, result.Error)
	assert.Contains(t, result.Error.Error(), "invalid API key")
}

func TestValidateAPIKey_429(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := MethodConfig{ValidationURL: server.URL}
	result := ValidateAPIKey(context.Background(), cfg, "sk-valid-but-limited")
	assert.True(t, result.Valid, "429 means key is valid, just rate limited")
}

func TestValidateAPIKey_AnthropicHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sk-ant-test", r.Header.Get("x-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"), "should not set Bearer for Anthropic")
		assert.Equal(t, "POST", r.Method)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"content":[{"text":"hi"}]}`))
	}))
	defer server.Close()

	cfg := MethodConfig{
		ValidationURL:    server.URL,
		ValidationMethod: "POST",
		AuthHeaderKey:    "x-api-key",
	}
	result := ValidateAPIKey(context.Background(), cfg, "sk-ant-test")
	assert.True(t, result.Valid)
}

func TestValidateAPIKey_GeminiQueryParam(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "AIza-test-key", r.URL.Query().Get("key"))
		assert.Empty(t, r.Header.Get("Authorization"), "should not set Bearer for Gemini")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"models":[]}`))
	}))
	defer server.Close()

	cfg := MethodConfig{
		ValidationURL: server.URL + "/v1beta/models",
		AuthHeaderKey: "query:key",
	}
	result := ValidateAPIKey(context.Background(), cfg, "AIza-test-key")
	assert.True(t, result.Valid)
}

func TestValidateAPIKey_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Small sleep to ensure context is expired
	time.Sleep(5 * time.Millisecond)

	cfg := MethodConfig{ValidationURL: "http://192.0.2.1:1234/never"} // RFC 5737 TEST-NET
	result := ValidateAPIKey(ctx, cfg, "sk-test")
	assert.NotNil(t, result.Error)
}
