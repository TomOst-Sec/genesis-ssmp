package auth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ValidationResult reports whether an API key is valid.
type ValidationResult struct {
	Valid  bool
	Error  error
	Detail string
}

// ValidateAPIKey makes a cheap API call to check if the key works.
func ValidateAPIKey(ctx context.Context, cfg MethodConfig, apiKey string) ValidationResult {
	if cfg.ValidationURL == "" {
		return ValidationResult{Valid: true, Detail: "no validation endpoint configured"}
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	endpoint := cfg.ValidationURL
	method := cfg.ValidationMethod
	if method == "" {
		method = "GET"
	}

	var req *http.Request
	var err error

	switch method {
	case "POST":
		// Anthropic: minimal messages request
		body := strings.NewReader(`{"model":"claude-3-haiku-20240307","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
		req, err = http.NewRequestWithContext(ctx, "POST", endpoint, body)
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("anthropic-version", "2023-06-01")
		}
	default:
		// query:key means append key as query param (Gemini)
		if strings.HasPrefix(cfg.AuthHeaderKey, "query:") {
			paramName := strings.TrimPrefix(cfg.AuthHeaderKey, "query:")
			if strings.Contains(endpoint, "?") {
				endpoint = endpoint + "&" + paramName + "=" + apiKey
			} else {
				endpoint = endpoint + "?" + paramName + "=" + apiKey
			}
		}
		req, err = http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	}

	if err != nil {
		return ValidationResult{Error: fmt.Errorf("build request: %w", err)}
	}

	// Set auth headers (skip for query-param auth)
	if strings.HasPrefix(cfg.AuthHeaderKey, "query:") {
		// Already handled above via URL
	} else if cfg.AuthHeaderKey != "" {
		req.Header.Set(cfg.AuthHeaderKey, apiKey)
	} else {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ValidationResult{Error: fmt.Errorf("request failed: %w", err)}
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return ValidationResult{Valid: false, Error: fmt.Errorf("invalid API key (HTTP %d)", resp.StatusCode)}
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return ValidationResult{Valid: true, Detail: fmt.Sprintf("HTTP %d OK", resp.StatusCode)}
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// Rate limited or server error — key is accepted, just busy
		return ValidationResult{Valid: true, Detail: fmt.Sprintf("key accepted (HTTP %d)", resp.StatusCode)}
	default:
		return ValidationResult{Valid: false, Error: fmt.Errorf("unexpected HTTP %d", resp.StatusCode)}
	}
}
