// Package mcpserver is the fork's hosted MCP server: a Streamable HTTP MCP
// endpoint that exposes the platform's catalog, pricing, account and chat
// abilities as tools for coding agents (Claude Code, Cursor, Codex...), the
// way OpenRouter's mcp.openrouter.ai does.
//
// Design rules that keep this package conflict-free with upstream new-api:
//   - Fork-only package; upstream is touched by exactly one line in
//     router/main.go (SetMcpRouter) plus the go.mod dependency.
//   - Tools never reach into relay/billing internals. Anything that consumes
//     quota or depends on the caller's visibility (models, chat) is dispatched
//     in-process through the gin engine to the platform's own REST API with the
//     caller's Authorization header, so auth, model allow-lists, reseller
//     routing, rate limits and billing all apply unchanged.
//   - Read-only lookups (pricing, vendors, credits, own generations, rankings)
//     read the same caches/tables the REST handlers read.
//
// Auth is the platform API key as a Bearer token (middleware.TokenAuth runs in
// front of the handler). The server is stateless: every POST is a temporary
// session, so no session store and no sticky routing are needed.
package mcpserver

import (
	"context"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ginContextKey carries the authenticated gin context into tool handlers. The
// SDK derives the tool handler's ctx from the HTTP request ctx, so a value set
// on the request before ServeHTTP is visible in every tool call of that POST.
type ginContextKey struct{}

func ginFrom(ctx context.Context) *gin.Context {
	c, _ := ctx.Value(ginContextKey{}).(*gin.Context)
	return c
}

// Handler returns the gin handler serving the MCP endpoint. engine is the
// application's own router, used for in-process dispatch to /v1 APIs.
func Handler(engine *gin.Engine) gin.HandlerFunc {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "accelsrouter",
		Title:   common.SystemName,
		Version: common.Version,
	}, &mcp.ServerOptions{
		Instructions: "Tools for exploring this AI gateway from a coding agent: browse the model catalog and " +
			"effective prices for your API key, check remaining credit, inspect a past generation by request id, " +
			"see which models are trending, and send a test message. Only send-message consumes credit.",
	})
	registerTools(server, engine)

	httpHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
	})
	return func(c *gin.Context) {
		req := c.Request.WithContext(context.WithValue(c.Request.Context(), ginContextKey{}, c))
		httpHandler.ServeHTTP(c.Writer, req)
	}
}
