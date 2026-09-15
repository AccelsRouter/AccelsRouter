package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// BlockResellerCustomer rejects money-in requests (self top-up, redeem code,
// subscription purchase) from a user who belongs to a reseller-provisioned
// customer org. Such users are downstream clients of a reseller: their credit
// is allocated by the reseller wallet, and the customer console hides these
// controls. This is the backend hard-gate behind that UI so the paths can't be
// reached by calling the API directly.
//
// Must run after UserAuth so the user id is populated. Fails OPEN: this gate
// backs a UI convenience (a customer's credit comes from the reseller, so its
// personal balance goes unused by org keys), not a hard money boundary, so a
// lookup hiccup must not turn into a platform-wide payment outage.
func BlockResellerCustomer() gin.HandlerFunc {
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "未登录",
			})
			c.Abort()
			return
		}
		isCustomer, err := model.IsResellerCustomerUser(userId)
		if err != nil {
			logger.LogError(c.Request.Context(), "reseller-customer gate lookup failed: "+err.Error())
			c.Next()
			return
		}
		if isCustomer {
			c.JSON(http.StatusForbidden, gin.H{
				"success": false,
				"code":    "RESELLER_CUSTOMER_FORBIDDEN",
				"message": "你的额度由代理商分配，请联系代理商充值",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
