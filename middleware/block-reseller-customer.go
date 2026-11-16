package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

// moneyInRule says how a user's organization takes money in: nothing special
// (a personal account, or an ordinary organization member), a prepaid reseller
// customer whose credit the reseller allocates, or a member of an organization
// that pays for itself, whose payments all go to the organization's wallet.
type moneyInRule struct {
	prepaidCustomer bool
	directFundedOrg int
	canTopUpOrg     bool
}

func moneyInRuleOf(userId int) (moneyInRule, error) {
	acc, err := model.GetOrgAccountByUser(userId)
	if err != nil || acc == nil {
		return moneyInRule{}, err
	}
	org, err := model.GetOrganizationById(acc.OrgId)
	if err != nil || org == nil {
		return moneyInRule{}, err
	}
	if org.FundingMode == model.OrgFundingDirect {
		return moneyInRule{
			directFundedOrg: org.Id,
			canTopUpOrg: org.Status == model.OrgStatusActive &&
				acc.Status != model.OrgStatusSuspended &&
				(acc.Role == model.OrgRoleOwner || acc.Role == model.OrgRoleAdmin),
		}, nil
	}
	return moneyInRule{prepaidCustomer: model.IsCustomerOrg(org.Id)}, nil
}

func refuseMoneyIn(c *gin.Context, message string) {
	common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage(message), gin.H{"code": "RESELLER_CUSTOMER_FORBIDDEN"})
	c.Abort()
}

// BlockResellerCustomer rejects money-in requests that have no place in an
// organization-funded account: redeem codes and subscription purchases from a
// user who belongs to a reseller-provisioned customer org, or to an
// organization that pays for itself. A prepaid customer's credit is allocated
// by the reseller; a self-funded organization's wallet takes online payments
// only (OrgTopUpGate), so that every unit in it was paid for. This is the
// backend hard-gate behind the hidden console controls, so the paths can't be
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
		rule, err := moneyInRuleOf(userId)
		if err != nil {
			logger.LogError(c.Request.Context(), "reseller-customer gate lookup failed: "+err.Error())
			c.Next()
			return
		}
		if rule.prepaidCustomer {
			refuseMoneyIn(c, "Your credit is allocated by your distributor; contact the distributor to add more")
			return
		}
		if rule.directFundedOrg > 0 {
			refuseMoneyIn(c, "Your organization's wallet is topped up by online payment only")
			return
		}
		c.Next()
	}
}

// OrgTopUpGate guards the online payment requests. A prepaid reseller customer
// is refused as before. In an organization that pays for itself, its owner and
// admins may pay, and the orders the request creates are noted as the
// organization's (the payment lands in the organization's wallet when it
// completes; see model.creditOrgTopUp); a plain member is refused, since a
// payment of theirs would land there too. Everyone else passes untouched.
//
// Must run after UserAuth. Fails OPEN like BlockResellerCustomer: where a
// completed payment lands is decided when it completes, not here.
func OrgTopUpGate() gin.HandlerFunc {
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
		rule, err := moneyInRuleOf(userId)
		if err != nil {
			logger.LogError(c.Request.Context(), "organization top-up gate lookup failed: "+err.Error())
			c.Next()
			return
		}
		if rule.prepaidCustomer {
			refuseMoneyIn(c, "Your credit is allocated by your distributor; contact the distributor to add more")
			return
		}
		if rule.directFundedOrg == 0 {
			c.Next()
			return
		}
		if !rule.canTopUpOrg {
			refuseMoneyIn(c, "Only an owner or admin of the organization can top up its wallet")
			return
		}
		since := common.GetTimestamp()
		c.Next()
		if err := model.RecordOrgTopUpOrders(rule.directFundedOrg, userId, since); err != nil {
			logger.LogError(c.Request.Context(), "failed to note the organization of a payment order: "+err.Error())
		}
	}
}
