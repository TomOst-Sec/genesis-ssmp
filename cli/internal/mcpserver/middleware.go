package mcpserver

import (
	"crypto/subtle"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// authMiddleware rejects requests without a valid Bearer token.
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "Unauthorized: missing Bearer token", http.StatusUnauthorized)
			return
		}
		provided := strings.TrimPrefix(auth, "Bearer ")
		if subtle.ConstantTimeCompare([]byte(provided), []byte(token)) != 1 {
			http.Error(w, "Unauthorized: invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware sets CORS headers and handles OPTIONS preflight.
func corsMiddleware(origins []string, next http.Handler) http.Handler {
	allowOrigin := "*"
	if len(origins) > 0 {
		allowOrigin = strings.Join(origins, ", ")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Mcp-Session-Id")
		w.Header().Set("Access-Control-Max-Age", "3600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// rateLimitMiddleware implements a simple token bucket rate limiter.
func rateLimitMiddleware(requestsPerMinute int, next http.Handler) http.Handler {
	var mu sync.Mutex
	tokens := requestsPerMinute
	lastRefill := time.Now()
	refillInterval := time.Minute

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		now := time.Now()
		elapsed := now.Sub(lastRefill)
		if elapsed >= refillInterval {
			tokens = requestsPerMinute
			lastRefill = now
		}
		if tokens <= 0 {
			mu.Unlock()
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		tokens--
		mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// loggingMiddleware logs each request with method, path, status, and duration.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher for SSE compatibility.
func (sw *statusWriter) Flush() {
	if f, ok := sw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("mcp %s %s %d %s", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
	})
}
