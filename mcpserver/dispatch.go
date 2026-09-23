package mcpserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/gin-gonic/gin"
)

// dispatchResult is the outcome of an in-process call to the platform's own
// REST API.
type dispatchResult struct {
	Status int
	Body   []byte
	Header http.Header
}

// dispatch runs a request against the application's own router without a
// network hop, as the MCP caller: the Authorization header and client address
// are forwarded, so the inner request is authenticated, rate limited, routed
// and billed exactly as if the client had called the REST API directly. Only
// fixed internal paths are ever dispatched (no caller-controlled URL).
func dispatch(ctx context.Context, engine *gin.Engine, method, path string, body []byte) (*dispatchResult, error) {
	outer := ginFrom(ctx)
	if outer == nil || engine == nil {
		return nil, fmt.Errorf("mcp: missing request context")
	}
	req, err := http.NewRequestWithContext(ctx, method, path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.RemoteAddr = outer.Request.RemoteAddr
	req.Host = outer.Request.Host
	for _, h := range []string{"Authorization", "Accept-Language", "User-Agent", "X-Forwarded-For", "X-Real-Ip", "Cf-Connecting-Ip"} {
		if v := outer.Request.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	rec := &recorder{ResponseRecorder: httptest.NewRecorder()}
	engine.ServeHTTP(rec, req)
	return &dispatchResult{Status: rec.Code, Body: rec.Body.Bytes(), Header: rec.Header()}, nil
}

// recorder adds CloseNotify to httptest.ResponseRecorder: gin's writer type-
// asserts http.CloseNotifier on the underlying writer, and a bare recorder
// would panic if any handler on the dispatched path asked for it.
type recorder struct {
	*httptest.ResponseRecorder
}

func (r *recorder) CloseNotify() <-chan bool { return make(chan bool) }
