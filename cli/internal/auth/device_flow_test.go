package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Speed up polling tests
	minPollInterval = 1
}

func TestRequestDeviceCode_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		r.ParseForm()
		assert.Equal(t, "test-client-id", r.FormValue("client_id"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceCodeResponse{
			DeviceCode:      "dc-123",
			UserCode:        "ABCD-1234",
			VerificationURI: "https://github.com/login/device",
			ExpiresIn:       900,
			Interval:        5,
		})
	}))
	defer server.Close()

	cfg := MethodConfig{
		DeviceAuthURL: server.URL,
		ClientID:      "test-client-id",
		Scopes:        "read:user",
	}
	code, err := RequestDeviceCode(context.Background(), cfg)
	require.NoError(t, err)
	assert.Equal(t, "dc-123", code.DeviceCode)
	assert.Equal(t, "ABCD-1234", code.UserCode)
	assert.Equal(t, "https://github.com/login/device", code.VerificationURI)
}

func TestRequestDeviceCode_MissingClientID(t *testing.T) {
	cfg := MethodConfig{
		DeviceAuthURL: "https://github.com/login/device/code",
		ClientID:      "",
	}
	_, err := RequestDeviceCode(context.Background(), cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no client_id")
}

func TestPollForToken_ImmediateSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			AccessToken: "gho_test_token_123",
			TokenType:   "bearer",
		})
	}))
	defer server.Close()

	cfg := MethodConfig{TokenURL: server.URL, ClientID: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := PollForToken(ctx, cfg, "dc-123", 1) // 1s interval for fast test
	require.NoError(t, err)
	assert.Equal(t, "gho_test_token_123", token.AccessToken)
}

func TestPollForToken_PendingThenSuccess(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := calls.Add(1)
		if n <= 2 {
			json.NewEncoder(w).Encode(DeviceTokenResponse{
				Error: "authorization_pending",
			})
			return
		}
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			AccessToken: "gho_after_pending",
			TokenType:   "bearer",
		})
	}))
	defer server.Close()

	cfg := MethodConfig{TokenURL: server.URL, ClientID: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := PollForToken(ctx, cfg, "dc-123", 1)
	require.NoError(t, err)
	assert.Equal(t, "gho_after_pending", token.AccessToken)
	assert.GreaterOrEqual(t, int(calls.Load()), 3)
}

func TestPollForToken_SlowDown(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		n := calls.Add(1)
		if n == 1 {
			json.NewEncoder(w).Encode(DeviceTokenResponse{
				Error: "slow_down",
			})
			return
		}
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			AccessToken: "gho_after_slowdown",
			TokenType:   "bearer",
		})
	}))
	defer server.Close()

	cfg := MethodConfig{TokenURL: server.URL, ClientID: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := PollForToken(ctx, cfg, "dc-123", 1)
	require.NoError(t, err)
	assert.Equal(t, "gho_after_slowdown", token.AccessToken)
}

func TestPollForToken_Expired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			Error: "expired_token",
		})
	}))
	defer server.Close()

	cfg := MethodConfig{TokenURL: server.URL, ClientID: "test"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := PollForToken(ctx, cfg, "dc-123", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expired")
}

func TestPollForToken_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(DeviceTokenResponse{
			Error: "authorization_pending",
		})
	}))
	defer server.Close()

	cfg := MethodConfig{TokenURL: server.URL, ClientID: "test"}
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately so the first tick triggers context done
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()

	_, err := PollForToken(ctx, cfg, "dc-123", 1)
	require.Error(t, err)
}
