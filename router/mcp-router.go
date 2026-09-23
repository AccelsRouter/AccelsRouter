package router

import (
	"github.com/QuantumNous/new-api/mcpserver"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

// SetMcpRouter mounts the fork's hosted MCP server (see package mcpserver) at
// /mcp. Clients authenticate with a platform API key as a Bearer token; the
// same TokenAuth as the relay applies, so disabled, expired or exhausted keys
// are refused before any tool runs.
func SetMcpRouter(router *gin.Engine) {
	mcpRouter := router.Group("/mcp")
	mcpRouter.Use(middleware.RouteTag("relay"))
	mcpRouter.Use(middleware.TokenAuth())
	mcpRouter.Any("", mcpserver.Handler(router))
}
