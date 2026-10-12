package middleware

import (
	"log"
	"os"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

func ConfigureTrustedProxies(engine *gin.Engine) error {
	// Fork-local: when CloudFront fronts the ALB, resolve the true viewer IP so
	// c.ClientIP() (login/register audit, last-login IP, IP rate limiting,
	// Turnstile) returns the end user, not the CloudFront edge IP. RealClientIP()
	// populates RealClientIPHeader from CloudFront-Viewer-Address; TrustedPlatform
	// makes gin's ClientIP() read that header first (falling back to the
	// X-Forwarded-For / RemoteAddr logic below when absent).
	engine.TrustedPlatform = RealClientIPHeader
	engine.Use(RealClientIP())

	trustedProxies, usedDefaults, err := common.ResolveTrustedProxies(os.Getenv("TRUSTED_PROXIES"))
	if err != nil {
		return err
	}
	if usedDefaults {
		log.Print(common.LogText("WARNING: TRUSTED_PROXIES is unset or blank; trusting loopback, RFC 1918, and IPv6 ULA proxy addresses for compatibility. Set TRUSTED_PROXIES=none to trust no proxies, or configure explicit proxy IPs/CIDRs to replace these defaults."))
	}
	return common.ConfigureTrustedProxies(engine, trustedProxies)
}
