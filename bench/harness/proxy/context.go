package main

import (
	"context"
	"net/http"
	"time"
)

type ctxKey int

const (
	ctxKeyBody ctxKey = iota
	ctxKeyStart
)

func setRequestBody(r *http.Request, body []byte) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyBody, body))
}

func getRequestBody(r *http.Request) []byte {
	if v, ok := r.Context().Value(ctxKeyBody).([]byte); ok {
		return v
	}
	return nil
}

func setRequestStart(r *http.Request, t time.Time) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKeyStart, t))
}

func getRequestStart(r *http.Request) time.Time {
	if v, ok := r.Context().Value(ctxKeyStart).(time.Time); ok {
		return v
	}
	return time.Now().UTC()
}
