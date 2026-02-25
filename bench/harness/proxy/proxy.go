package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// ProxyServer intercepts HTTP traffic, records API usage, and forwards to upstream.
type ProxyServer struct {
	target   *url.URL
	recorder *Recorder
	proxy    *httputil.ReverseProxy
	saveBodies bool
}

// NewProxyServer creates a transparent reverse proxy that records traffic.
func NewProxyServer(targetURL string, recorder *Recorder, saveBodies bool) (*ProxyServer, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	ps := &ProxyServer{
		target:     target,
		recorder:   recorder,
		saveBodies: saveBodies,
	}

	ps.proxy = &httputil.ReverseProxy{
		Director:       ps.director,
		ModifyResponse: ps.modifyResponse,
		ErrorHandler:   ps.errorHandler,
	}

	return ps, nil
}

// director rewrites the request to point at the upstream target.
func (ps *ProxyServer) director(req *http.Request) {
	req.URL.Scheme = ps.target.Scheme
	req.URL.Host = ps.target.Host
	req.Host = ps.target.Host
}

// modifyResponse captures the response, extracts usage, and records it.
func (ps *ProxyServer) modifyResponse(resp *http.Response) error {
	// Only record /v1/messages calls
	if !strings.HasPrefix(resp.Request.URL.Path, "/v1/messages") {
		return nil
	}

	start := getRequestStart(resp.Request)
	end := time.Now().UTC()
	latency := end.Sub(start).Milliseconds()

	// Read request body from context (captured in ServeHTTP wrapper)
	reqBody := getRequestBody(resp.Request)

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	// Parse request metadata
	var reqMeta struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
	}
	json.Unmarshal(reqBody, &reqMeta)

	// Extract usage from response
	usage := extractUsage(respBody, resp.Header.Get("Content-Type"))

	rec := TrafficRecord{
		CallSeq:        ps.recorder.NextSeq(),
		TimestampStart: start.Format(time.RFC3339Nano),
		TimestampEnd:   end.Format(time.RFC3339Nano),
		LatencyMS:      latency,
		Method:         resp.Request.Method,
		Path:           resp.Request.URL.Path,
		StatusCode:     resp.StatusCode,
		RequestBytes:   len(reqBody),
		ResponseBytes:  len(respBody),

		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		CacheCreationInputTokens: usage.CacheCreationInputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,

		Model:     reqMeta.Model,
		MaxTokens: reqMeta.MaxTokens,
		Stream:    reqMeta.Stream,
	}

	if ps.saveBodies {
		rec.RequestBodyGz = compressB64(reqBody)
		rec.ResponseBodyGz = compressB64(respBody)
	}

	if err := ps.recorder.Record(rec); err != nil {
		log.Printf("recorder error: %v", err)
	}

	return nil
}

func (ps *ProxyServer) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("proxy error: %v", err)
	w.WriteHeader(http.StatusBadGateway)
}

// Handler returns an http.Handler that captures request bodies before proxying.
func (ps *ProxyServer) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Capture request body before proxy consumes it
		var reqBody []byte
		if r.Body != nil {
			reqBody, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(reqBody))
		}

		// Store in context via request header (simple approach)
		r = setRequestBody(r, reqBody)
		r = setRequestStart(r, time.Now().UTC())

		ps.proxy.ServeHTTP(w, r)
	})

	return mux
}

// apiUsage represents the usage block from an Anthropic API response.
type apiUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// extractUsage parses usage from a JSON response or SSE stream.
func extractUsage(body []byte, contentType string) apiUsage {
	if strings.Contains(contentType, "text/event-stream") {
		return extractSSEUsage(body)
	}
	return extractJSONUsage(body)
}

// extractJSONUsage extracts usage from a standard JSON response body.
func extractJSONUsage(body []byte) apiUsage {
	var resp struct {
		Usage apiUsage `json:"usage"`
	}
	json.Unmarshal(body, &resp)
	return resp.Usage
}

// extractSSEUsage parses an SSE stream for the final message_delta event's usage.
func extractSSEUsage(body []byte) apiUsage {
	var cumulative apiUsage
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		eventType := strings.TrimPrefix(line, "event: ")

		// Find corresponding data line
		dataLine := ""
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "data: ") {
				dataLine = strings.TrimPrefix(lines[j], "data: ")
				break
			}
		}
		if dataLine == "" {
			continue
		}

		switch eventType {
		case "message_start":
			var msg struct {
				Message struct {
					Usage apiUsage `json:"usage"`
				} `json:"message"`
			}
			if json.Unmarshal([]byte(dataLine), &msg) == nil {
				cumulative.InputTokens = msg.Message.Usage.InputTokens
				cumulative.CacheCreationInputTokens = msg.Message.Usage.CacheCreationInputTokens
				cumulative.CacheReadInputTokens = msg.Message.Usage.CacheReadInputTokens
			}
		case "message_delta":
			var delta struct {
				Usage struct {
					OutputTokens int64 `json:"output_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal([]byte(dataLine), &delta) == nil {
				cumulative.OutputTokens = delta.Usage.OutputTokens
			}
		}
	}
	return cumulative
}

// compressB64 gzip-compresses data and returns base64-encoded result.
func compressB64(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write(data)
	gz.Close()
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}
