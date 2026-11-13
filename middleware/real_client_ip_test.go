package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPFromViewerAddress(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"ipv4 with port", "203.0.113.5:1234", "203.0.113.5"},
		{"ipv6 with port", "2001:db8::1:1234", "2001:db8::1"},
		{"ipv6 loopback with port", "::1:5678", "::1"},
		{"ipv6 full with port", "2001:db8:0:0:0:0:0:1:443", "2001:db8:0:0:0:0:0:1"},
		{"bare ipv4 no port", "203.0.113.5", "203.0.113.5"},
		{"bare ipv6 no port", "2001:db8::1", "2001:db8::1"},
		{"empty", "", ""},
		{"garbage", "not-an-ip:1234", ""},
		{"port only", ":1234", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, ipFromViewerAddress(tc.in))
		})
	}
}

// The CloudFront viewer address is believed only from a trusted proxy: a client
// connecting by itself cannot name another address as its own, and can never
// set the internal header directly.
func TestRealClientIPBelievesTheViewerHeaderOnlyFromATrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clientIP := func(remoteAddr string, trusted []string) string {
		engine := gin.New()
		engine.TrustedPlatform = RealClientIPHeader
		engine.Use(RealClientIP(trusted))
		require.NoError(t, engine.SetTrustedProxies(trusted))
		var seen string
		engine.GET("/", func(c *gin.Context) { seen = c.ClientIP() })
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		req.Header.Set("CloudFront-Viewer-Address", "203.0.113.5:4321")
		req.Header.Set(RealClientIPHeader, "198.51.100.9")
		engine.ServeHTTP(httptest.NewRecorder(), req)
		return seen
	}
	assert.Equal(t, "203.0.113.5", clientIP("10.1.2.3:5000", nil), "behind a balancer the viewer address is the client, whatever TRUSTED_PROXIES says")
	assert.Equal(t, "203.0.113.5", clientIP("192.0.2.10:5000", []string{"192.0.2.10"}), "a listed public proxy counts too")
	assert.Equal(t, "192.0.2.77", clientIP("192.0.2.77:5000", []string{"10.0.0.0/8"}), "a direct client is its own address, whatever it claims")
	assert.Equal(t, "192.0.2.77", clientIP("192.0.2.77:5000", nil), "and so it is with no proxy trusted at all")
}
