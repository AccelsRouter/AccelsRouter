package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets for the supported
// themes (default = upstream web/, plus the aurora theme). Classic was
// removed upstream in v1.0.0-rc.22.
type WebAssets struct {
	DefaultBuildFS   embed.FS
	DefaultIndexPage []byte
	AuroraBuildFS    embed.FS
	AuroraIndexPage  []byte
}

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	defaultFS := common.EmbedFolder(assets.DefaultBuildFS, "web/dist")
	auroraFS := common.EmbedFolder(assets.AuroraBuildFS, "aurora/dist")
	frontendFS := common.NewThemeAwareFS(defaultFS, auroraFS)
	compress := gzip.Gzip(gzip.DefaultCompression)

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		func(c *gin.Context) {
			// ServeFrontendFiles sends build files already compressed.
			if !frontendFS.Exists("/", c.Request.URL.Path) {
				compress(c)
			}
		},
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(frontendFS),
		middleware.Cache(),
		middleware.ServeFrontendFiles(frontendFS),
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
				controller.RelayNotFound(c)
				return
			}
			c.Header("Cache-Control", "no-cache")
			// Fork: the runtime theme switch picks which embedded SPA answers.
			if common.GetTheme() == "aurora" {
				c.Data(http.StatusOK, "text/html; charset=utf-8", assets.AuroraIndexPage)
			} else {
				c.Data(http.StatusOK, "text/html; charset=utf-8", assets.DefaultIndexPage)
			}
		},
	)
}
