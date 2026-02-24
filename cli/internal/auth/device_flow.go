package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCodeResponse is returned when initiating the device authorization flow.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// DeviceTokenResponse is returned when polling for the access token.
type DeviceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// RequestDeviceCode initiates the device flow by requesting a user code.
func RequestDeviceCode(ctx context.Context, cfg MethodConfig) (*DeviceCodeResponse, error) {
	if cfg.DeviceAuthURL == "" {
		return nil, fmt.Errorf("device flow not configured: no device auth URL")
	}
	if cfg.ClientID == "" {
		return nil, fmt.Errorf("device flow not configured: no client_id (register a GitHub OAuth App first)")
	}

	data := url.Values{
		"client_id": {cfg.ClientID},
		"scope":     {cfg.Scopes},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.DeviceAuthURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request device code: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code request failed: HTTP %d", resp.StatusCode)
	}

	var result DeviceCodeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode device code: %w", err)
	}
	if result.Interval < 5 {
		result.Interval = 5
	}
	return &result, nil
}

// minPollInterval is the minimum polling interval in seconds. Tests override this.
var minPollInterval = 5

// PollForToken polls the token endpoint until the user authorizes,
// the code expires, or the context is cancelled.
func PollForToken(ctx context.Context, cfg MethodConfig, deviceCode string, interval int) (*DeviceTokenResponse, error) {
	if interval < minPollInterval {
		interval = minPollInterval
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			token, err := pollOnce(ctx, cfg, deviceCode)
			if err != nil {
				return nil, err
			}

			switch token.Error {
			case "":
				return token, nil
			case "authorization_pending":
				continue
			case "slow_down":
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired — please restart the flow")
			case "access_denied":
				return nil, fmt.Errorf("authorization denied by user")
			default:
				return nil, fmt.Errorf("device flow error: %s — %s", token.Error, token.ErrorDesc)
			}
		}
	}
}

func pollOnce(ctx context.Context, cfg MethodConfig, deviceCode string) (*DeviceTokenResponse, error) {
	data := url.Values{
		"client_id":   {cfg.ClientID},
		"device_code": {deviceCode},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", cfg.TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var token DeviceTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&token); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	return &token, nil
}
