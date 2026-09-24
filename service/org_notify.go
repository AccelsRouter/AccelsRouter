package service

// Fork-only organization budget notifications. When a request is blocked by an
// org budget/wallet gate, the org owner is alerted so they can top up or raise
// a budget. Delivery reuses the platform's per-user notification channel
// (email/webhook/bark/gotify) and its built-in hourly, per-type cooldown
// (NotifyUser -> CheckNotificationLimit), so a burst of blocked requests sends
// at most one message per hour per reason. Everything runs off the hot path in
// a goroutine and is best-effort.

import (
	"fmt"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/bytedance/gopkg/util/gopool"
)

// Distinct notify types keep each reason's cooldown independent and separate
// from the owner's personal quota reminders.
const (
	notifyOrgWalletInsufficient = "org_wallet_insufficient"
	notifyOrgMemberBudget       = "org_member_budget"
	notifyOrgWorkspaceBudget    = "org_workspace_budget"
)

// orgNotifyThrottle collapses a storm of blocked requests before any work is
// done: it caps attempts to one per org+reason per window, so a burst does not
// spawn a goroutine + two DB reads per blocked request. Actual delivery is
// still gated by NotifyUser's hourly per-user cooldown; this only avoids the
// upstream amplification. The tiny map (bounded by active orgs) is acceptable.
var (
	orgNotifyThrottle    sync.Map // "orgId:reason" -> time.Time of last attempt
	orgNotifyThrottleTTL = 5 * time.Minute
)

func shouldAttemptOrgNotify(orgId int, reason string) bool {
	key := fmt.Sprintf("%d:%s", orgId, reason)
	now := time.Now()
	if v, ok := orgNotifyThrottle.Load(key); ok {
		if now.Sub(v.(time.Time)) < orgNotifyThrottleTTL {
			return false
		}
	}
	orgNotifyThrottle.Store(key, now)
	return true
}

// notifyOrgOwnerBudget alerts the org owner about a budget/wallet block.
// reason is one of "wallet" | "member" | "workspace".
func notifyOrgOwnerBudget(orgId int, reason string) {
	if !shouldAttemptOrgNotify(orgId, reason) {
		return
	}
	var notifyType, prompt string
	switch reason {
	case "wallet":
		notifyType, prompt = notifyOrgWalletInsufficient, "组织钱包余额不足"
	case "member":
		notifyType, prompt = notifyOrgMemberBudget, "组织成员本月预算已用尽"
	case "workspace":
		notifyType, prompt = notifyOrgWorkspaceBudget, "组织 workspace 本月预算已用尽"
	default:
		return
	}
	gopool.Go(func() {
		org, err := model.GetOrganizationById(orgId)
		if err != nil || org == nil || org.OwnerUserId == 0 {
			return
		}
		owner, err := model.GetUserById(org.OwnerUserId, false)
		if err != nil || owner == nil {
			return
		}
		title := prompt
		content, values := orgBudgetContent(owner.GetSetting().NotifyType, org.Name, prompt)
		if err := NotifyUser(owner.Id, owner.Email, owner.GetSetting(), dto.NewNotify(notifyType, title, content, values)); err != nil {
			// A limit-exceeded "error" is the expected cooldown, not a fault;
			// log at info level via SysLog so it stays quiet.
			logger.LogInfo(nil, fmt.Sprintf("org budget notify skipped for org %d (%s): %s", orgId, reason, err.Error()))
		}
	})
}

// orgBudgetContent renders the message body per delivery channel (HTML for
// email/webhook, plain for bark/gotify).
func orgBudgetContent(notifyType, orgName, prompt string) (string, []interface{}) {
	switch notifyType {
	case dto.NotifyTypeBark, dto.NotifyTypeGotify:
		return "组织「{{value}}」{{value}}，请及时处理。", []interface{}{orgName, prompt}
	default:
		return "组织「{{value}}」{{value}}，为了不影响使用，请及时充值或调整预算。", []interface{}{orgName, prompt}
	}
}
