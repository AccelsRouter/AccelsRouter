package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

// smartRouteSeed is the per-request seed handed to smart routing's
// ordering (see common/smartroute.RankChannels): the request ID, which is
// the same for every attempt of one request, so a retry follows the same
// order as the first attempt instead of re-drawing and possibly landing on
// the channel that just failed. Empty (no request ID available) falls
// back to a fresh random order per call.
func smartRouteSeed(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return c.GetString(common.RequestIdKey)
}
