package service

// Fork-only organization funding source (design doc: "wrouter 组织与分销架构").
// OpenRouter-style key-level billing: a request bills the organization that
// owns the workspace its API token is bound to. Three enforcement tiers,
// matching OpenRouter (key limit → workspace budget → org credits): the token
// remain-quota (existing pre-consume), the workspace monthly budget, and the
// org wallet. A per-seat member budget is an optional extra gate. Plugs into
// the existing FundingSource seam next to WalletFunding and SubscriptionFunding.
//
// Invariants:
//   - Reserve is a single atomic conditional UPDATE on the org wallet; an
//     insufficient balance never mutates anything.
//   - Member and workspace budgets are enforced at reserve time (hard gate);
//     the settle-overshoot path records spend past the budget instead of
//     dropping the debt, so budgets throttle FUTURE requests, never erase
//     consumed cost.
//   - Every failure path unwinds its own partial counters.

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

const BillingSourceOrgWallet = "org_wallet"

var (
	errOrgBudgetExceeded       = errors.New("org member monthly budget exceeded")
	errWorkspaceBudgetExceeded = errors.New("workspace monthly budget exceeded")
	// Distinct from ErrInsufficientWalletQuota on purpose: preConsume rewrites
	// that sentinel into a personal-quota message, which would mislead managed
	// users; this one flows through untouched and is mapped below.
	errOrgWalletInsufficient = errors.New("organization wallet quota insufficient")
	// Route-2: the reseller's own cost balance (its wallet) could not cover the
	// per-model wholesale cost of this call, so the platform cannot be paid — the
	// call is refused rather than served at a loss.
	errResellerBalanceInsufficient = errors.New("reseller cost balance insufficient")
)

// OrgWalletFunding charges the managing organization's wallet.
type OrgWalletFunding struct {
	orgId       int
	userId      int
	workspaceId int // 0 = token not bound to a workspace
	consumed    int // reserved org quota (for refund), in charged (discounted) units
	// discountRatio in (0,1] is the reseller's per-model-series retail discount
	// for THIS request's model (1.0 = none). It scales what the org wallet and
	// the member/workspace budget counters are charged, and nothing else — the
	// platform's price computation, consume log and channel stats stay standard.
	// This is the only place a reseller customer's discount reaches actual money.
	discountRatio float64
	// Route-2 (two-pool billing): when this customer belongs to a reseller,
	// resellerOrgId is that reseller's org and wholesaleRatio is the per-model
	// wholesale ratio (in (0,1]; 1.0 = reseller pays full standard). Each call
	// debits the reseller's wallet by standard × wholesaleRatio in addition to
	// the customer wallet at retail. resellerConsumed tracks the reserved amount
	// so Refund/Settle can unwind it exactly (same discipline as `consumed`).
	resellerOrgId    int
	wholesaleRatio   float64
	resellerConsumed int
}

func (o *OrgWalletFunding) Source() string { return BillingSourceOrgWallet }

// charge scales a standard quota amount by the retail discount, rounding
// (half-away-from-zero) via the shared quota-math helper (never a bare cast) so
// the charged amount stays closest to standard × ratio — truncating made a 0.4
// discount read as ~0.35 on tiny requests. Sign is preserved so a refund delta
// discounts symmetrically. A ratio outside (0,1] means no discount.
func (o *OrgWalletFunding) charge(amount int) int {
	if o.discountRatio <= 0 || o.discountRatio >= 1 {
		return amount
	}
	return common.QuotaRound(float64(amount) * o.discountRatio)
}

// wholesaleCharge scales a standard amount by the reseller's per-model wholesale
// ratio — what the reseller pays the platform for this call. A ratio outside
// (0,1) means no wholesale discount, i.e. the reseller pays the full standard
// amount. Uses the shared rounding helper (never a bare cast); sign preserved so
// a settle refund unwinds symmetrically.
func (o *OrgWalletFunding) wholesaleCharge(amount int) int {
	if o.wholesaleRatio <= 0 || o.wholesaleRatio >= 1 {
		return amount
	}
	return common.QuotaRound(float64(amount) * o.wholesaleRatio)
}

func (o *OrgWalletFunding) PreConsume(stdAmount int) error {
	amount := o.charge(stdAmount)
	if amount <= 0 {
		return nil
	}
	// 1. Member budget gate.
	ok, err := model.AddOrgAccountSpend(o.orgId, o.userId, amount, true)
	if err != nil {
		return err
	}
	if !ok {
		return errOrgBudgetExceeded
	}
	// 2. Workspace budget gate (unwind member counter on failure).
	ok, err = model.AddWorkspaceSpend(o.workspaceId, amount, true)
	if err == nil && !ok {
		err = errWorkspaceBudgetExceeded
	}
	if err != nil {
		if rErr := model.ReduceOrgAccountSpend(o.orgId, o.userId, amount); rErr != nil {
			common.SysError("org funding: unwind member spend failed: " + rErr.Error())
		}
		return err
	}
	// 3. Atomic wallet reserve (unwind both counters on failure).
	reserved, err := model.TryReserveOrgQuota(o.orgId, amount)
	if err == nil && !reserved {
		err = errOrgWalletInsufficient
	}
	if err != nil {
		if rErr := model.ReduceOrgAccountSpend(o.orgId, o.userId, amount); rErr != nil {
			common.SysError("org funding: unwind member spend failed: " + rErr.Error())
		}
		if rErr := model.ReduceWorkspaceSpend(o.workspaceId, amount); rErr != nil {
			common.SysError("org funding: unwind workspace spend failed: " + rErr.Error())
		}
		return err
	}
	// 4. Reseller cost balance reserve (route-2 two-pool). The reseller pays the
	// platform at the per-model wholesale ratio. On failure, unwind the customer
	// wallet reserve and both budget counters so nothing is left charged.
	if o.resellerOrgId > 0 {
		wAmount := o.wholesaleCharge(stdAmount)
		if wAmount > 0 {
			reservedR, rErr := model.TryReserveOrgQuota(o.resellerOrgId, wAmount)
			if rErr == nil && !reservedR {
				rErr = errResellerBalanceInsufficient
			}
			if rErr != nil {
				if e := model.IncreaseOrgQuota(o.orgId, amount); e != nil {
					common.SysError("org funding: unwind customer wallet reserve failed: " + e.Error())
				}
				if e := model.ReduceOrgAccountSpend(o.orgId, o.userId, amount); e != nil {
					common.SysError("org funding: unwind member spend failed: " + e.Error())
				}
				if e := model.ReduceWorkspaceSpend(o.workspaceId, amount); e != nil {
					common.SysError("org funding: unwind workspace spend failed: " + e.Error())
				}
				return rErr
			}
			o.resellerConsumed = wAmount
		}
	}
	o.consumed = amount
	return nil
}

func (o *OrgWalletFunding) Settle(stdDelta int) error {
	delta := o.charge(stdDelta)
	// Customer wallet + budgets (retail side).
	if delta != 0 {
		// Track the charged amount so a later Refund (only reachable before Settle,
		// e.g. a failed mid-stream reserve) returns exactly what was charged. This
		// is why billing_session.go must NOT also adjust o.consumed for org funding.
		o.consumed += delta
		if delta > 0 {
			// Overshoot: the upstream tokens are consumed; record unconditionally.
			if err := model.DecreaseOrgQuota(o.orgId, delta); err != nil {
				return err
			}
			if _, err := model.AddOrgAccountSpend(o.orgId, o.userId, delta, false); err != nil {
				common.SysError("org funding: settle member spend failed: " + err.Error())
			}
			if _, err := model.AddWorkspaceSpend(o.workspaceId, delta, false); err != nil {
				common.SysError("org funding: settle workspace spend failed: " + err.Error())
			}
		} else {
			refund := -delta
			if err := model.IncreaseOrgQuota(o.orgId, refund); err != nil {
				return err
			}
			if err := model.ReduceOrgAccountSpend(o.orgId, o.userId, refund); err != nil {
				common.SysError("org funding: settle member spend reduce failed: " + err.Error())
			}
			if err := model.ReduceWorkspaceSpend(o.workspaceId, refund); err != nil {
				common.SysError("org funding: settle workspace spend reduce failed: " + err.Error())
			}
		}
	}
	// Reseller cost balance (wholesale side) — route-2. Mirrors the customer
	// adjustment on the reseller wallet; resellerConsumed tracks it for Refund.
	if o.resellerOrgId > 0 {
		wDelta := o.wholesaleCharge(stdDelta)
		if wDelta != 0 {
			o.resellerConsumed += wDelta
			if wDelta > 0 {
				if err := model.DecreaseOrgQuota(o.resellerOrgId, wDelta); err != nil {
					return err
				}
			} else if err := model.IncreaseOrgQuota(o.resellerOrgId, -wDelta); err != nil {
				return err
			}
		}
	}
	return nil
}

func (o *OrgWalletFunding) Refund() error {
	// Customer wallet + budgets.
	if o.consumed > 0 {
		// Like WalletFunding.Refund: quota += N is non-idempotent, never retry.
		if err := model.IncreaseOrgQuota(o.orgId, o.consumed); err != nil {
			return err
		}
		if err := model.ReduceOrgAccountSpend(o.orgId, o.userId, o.consumed); err != nil {
			common.SysError("org funding: refund member spend failed: " + err.Error())
		}
		if err := model.ReduceWorkspaceSpend(o.workspaceId, o.consumed); err != nil {
			common.SysError("org funding: refund workspace spend failed: " + err.Error())
		}
		o.consumed = 0
	}
	// Reseller cost balance (route-2): return the reserved wholesale amount.
	if o.resellerOrgId > 0 && o.resellerConsumed > 0 {
		if err := model.IncreaseOrgQuota(o.resellerOrgId, o.resellerConsumed); err != nil {
			return err
		}
		o.resellerConsumed = 0
	}
	return nil
}

// tryOrgBillingSession implements OpenRouter-style key-level billing: a
// request bills an organization ONLY when its API token is bound to that
// org's workspace. A personal/unbound token returns (nil, nil) and the caller
// proceeds with the user's own wallet/subscription — so a user keeps a
// personal balance for personal keys and never has it silently bypassed by
// org membership. When the token IS workspace-bound, the org wallet pays and
// there is no fallback to personal funds (that would leak the org's cost onto
// the member); a suspended org/workspace, a suspended owning reseller (its
// customers stop with it) or an empty org wallet aborts.
func tryOrgBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	info, err := model.GetWorkspaceBillingInfo(relayInfo.TokenId)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if info == nil {
		return nil, nil // token not bound to a workspace — personal billing
	}
	if info.OrgStatus != model.OrgStatusActive {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("所属组织已被暂停"),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if info.WorkspaceStatus == model.OrgStatusSuspended {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("该 workspace 已被暂停"),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	// A reseller's customers stop with the reseller: while the owning reseller is
	// suspended, none of its customer orgs may consume, regardless of their own
	// status or wallet. Resumes automatically once the reseller is active again.
	if info.ResellerOrgId > 0 && info.ResellerOrgStatus != model.OrgStatusActive {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("所属分销商已被暂停"),
			types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
			types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}

	// Reseller retail discount: scale THIS customer's org-wallet charge by the
	// per-model-series ratio the reseller set (1.0 = none). Reporting only for a
	// non-reseller enterprise org (empty RetailDiscounts). Model matched on the
	// requested name, same as the usage-report overlay (RetailDiscountFor).
	discountRatio := 1.0
	if info.RetailDiscounts != "" {
		discountRatio = model.RetailDiscountFor(
			relayInfo.OriginModelName,
			model.ParseRetailDiscounts(info.RetailDiscounts),
		)
	}
	// Surface the applied discount so the consume log can show the discounted
	// price (see attachOrgRetailDiscount).
	relayInfo.OrgDiscountRatio = discountRatio
	// Route-2: resolve the reseller's per-model wholesale ratio for this call, so
	// the funding source also debits the reseller wallet at wholesale. The
	// wholesale map is parsed with the same {token->ratio} validator as retail.
	wholesaleRatio := 1.0
	if info.ResellerOrgId > 0 && info.WholesaleRatios != "" {
		wholesaleRatio = model.WholesaleRatioFor(
			relayInfo.OriginModelName,
			model.ParseRetailDiscounts(info.WholesaleRatios),
		)
	}
	// Carry the org-billing context so the settle path can record the immutable
	// usage rollup (org_usage_daily) with actual standard/charged/cost amounts.
	relayInfo.OrgId = info.OrgId
	relayInfo.OrgWorkspaceId = info.WorkspaceId
	relayInfo.ResellerOrgId = info.ResellerOrgId
	relayInfo.OrgWholesaleRatio = wholesaleRatio
	session := &BillingSession{
		relayInfo: relayInfo,
		funding: &OrgWalletFunding{
			orgId:          info.OrgId,
			userId:         relayInfo.UserId,
			workspaceId:    info.WorkspaceId,
			discountRatio:  discountRatio,
			resellerOrgId:  info.ResellerOrgId,
			wholesaleRatio: wholesaleRatio,
		},
	}
	if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
		// Alert the org owner about the block (best-effort, rate-limited).
		switch {
		case errors.Is(apiErr.Err, errOrgWalletInsufficient):
			notifyOrgOwnerBudget(info.OrgId, "wallet")
		case errors.Is(apiErr.Err, errOrgBudgetExceeded):
			notifyOrgOwnerBudget(info.OrgId, "member")
		case errors.Is(apiErr.Err, errWorkspaceBudgetExceeded):
			notifyOrgOwnerBudget(info.OrgId, "workspace")
		case errors.Is(apiErr.Err, errResellerBalanceInsufficient):
			// The reseller (upstream) is out of balance; alert the reseller owner.
			notifyOrgOwnerBudget(info.ResellerOrgId, "wallet")
		}
		return nil, orgFundingError(apiErr)
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("org billing: token %d (user %d) charged to org %d workspace %d", relayInfo.TokenId, relayInfo.UserId, info.OrgId, info.WorkspaceId))
	return session, nil
}

// orgFundingError maps funding failures to user-readable 403s.
func orgFundingError(apiErr *types.NewAPIError) *types.NewAPIError {
	msg := apiErr.Error()
	switch {
	case errors.Is(apiErr.Err, errOrgBudgetExceeded):
		msg = "本月组织预算已用尽，请联系组织管理员调整"
	case errors.Is(apiErr.Err, errWorkspaceBudgetExceeded):
		msg = "本月 workspace 预算已用尽，请联系组织管理员调整"
	case errors.Is(apiErr.Err, errOrgWalletInsufficient):
		msg = "组织钱包余额不足，请联系组织管理员充值"
	case errors.Is(apiErr.Err, errResellerBalanceInsufficient):
		msg = "上游分销商余额不足，请联系分销商充值"
	default:
		return apiErr
	}
	return types.NewErrorWithStatusCode(
		errors.New(msg),
		types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
		types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
}
