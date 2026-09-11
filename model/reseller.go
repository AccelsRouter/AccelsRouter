// Fork-only reseller (distributor) customer management, layered on the existing
// org credit primitives. A reseller is an Organization of type "reseller" that
// provisions downstream CUSTOMER orgs and funds them from its own wallet via
// the append-only credit_ledger. There is no parent link on Organization — the
// reseller⇄customer relationship lives only in the ledger, so request-time
// billing stays single-hop (a customer's own wallet pays for its usage). Margin
// is realized off-platform: the reseller buys quota at wholesale and its
// customers consume at their own (retail) price group.
package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// ParseRetailDiscounts parses a customer org's retail discount map {token ->
// ratio}. Invalid/empty yields an empty map (no discount).
func ParseRetailDiscounts(s string) map[string]float64 {
	s = strings.TrimSpace(s)
	out := map[string]float64{}
	if s == "" {
		return out
	}
	var raw map[string]float64
	if err := common.Unmarshal([]byte(s), &raw); err != nil {
		return out
	}
	for token, ratio := range raw {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" && ratio > 0 && ratio <= 1 {
			out[token] = ratio
		}
	}
	return out
}

// MarshalRetailDiscounts serializes a validated discount map for storage. An
// empty map serializes to "".
func MarshalRetailDiscounts(m map[string]float64) (string, error) {
	clean := map[string]float64{}
	for token, ratio := range m {
		token = strings.ToLower(strings.TrimSpace(token))
		if token != "" && ratio > 0 && ratio <= 1 {
			clean[token] = ratio
		}
	}
	if len(clean) == 0 {
		return "", nil
	}
	b, err := common.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RetailDiscountFor returns the ratio to apply to a model, matching the longest
// series token that is a substring of the (lowercased) model name; 1.0 (no
// discount) when nothing matches.
func RetailDiscountFor(modelName string, discounts map[string]float64) float64 {
	if len(discounts) == 0 {
		return 1.0
	}
	name := strings.ToLower(modelName)
	best := 1.0
	bestLen := -1
	for token, ratio := range discounts {
		if strings.Contains(name, token) && len(token) > bestLen {
			best = ratio
			bestLen = len(token)
		}
	}
	return best
}

// EffectiveWholesaleRatio returns the reseller's wholesale price ratio, clamped
// to the sane (0,1] range; anything else (unset 0, or a nonsensical >1) means
// "no discount" = 1.0. personal quota spent = purchased credit × ratio.
func (org *Organization) EffectiveWholesaleRatio() float64 {
	if org.WholesaleRatio > 0 && org.WholesaleRatio <= 1 {
		return org.WholesaleRatio
	}
	return 1.0
}

// PurchaseResellerCredit atomically buys wallet credit for a reseller org by
// debiting the purchasing user's personal quota. `cost` (personal quota spent)
// is computed by the caller from `quota` and the reseller's wholesale ratio.
// Both movements commit together; the user debit is conditional on sufficient
// balance so it can never drive the balance negative under concurrency.
func PurchaseResellerCredit(resellerOrgId, userId, quota, cost int, tradeNo, remark string) error {
	if quota <= 0 || cost <= 0 {
		return errors.New("购买额度必须为正")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&User{}).Where("id = ? AND quota >= ?", userId, cost).
			Update("quota", gorm.Expr("quota - ?", cost))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return errors.New("个人余额不足")
		}
		cred := tx.Model(&Organization{}).Where("id = ?", resellerOrgId).
			Update("wallet_quota", gorm.Expr("wallet_quota + ?", quota))
		if cred.Error != nil {
			return cred.Error
		}
		// Verify the credit landed (parity with PlatformCreditOrg/TransferOrgCredit):
		// if the org row is gone, roll back rather than silently debiting the buyer.
		if cred.RowsAffected != 1 {
			return errors.New("代理商组织不存在")
		}
		return insertLedger(tx, 0, resellerOrgId, quota, userId, LedgerTypePurchase, tradeNo, remark)
	})
	if err != nil {
		return err
	}
	// Keep the user-quota cache consistent with the committed DB debit.
	if cerr := cacheDecrUserQuota(userId, int64(cost)); cerr != nil {
		common.SysLog("reseller purchase: cache decr failed: " + cerr.Error())
	}
	return nil
}

// ResellerCustomerLink is the explicit reseller⇄customer relationship. It is
// the AUTHORIZATION record (not the ledger): a reseller may fund/view only an
// org linked to it here. CustomerOrgId is UNIQUE — a customer belongs to at
// most one reseller. This is intentionally NOT a field on Organization and is
// never consulted in the request-time billing path, so billing stays single-
// hop; it only scopes reseller-console access.
type ResellerCustomerLink struct {
	Id            int   `json:"id" gorm:"primarykey"`
	ResellerOrgId int   `json:"reseller_org_id" gorm:"index;not null"`
	CustomerOrgId int   `json:"customer_org_id" gorm:"uniqueIndex;not null"`
	CreatedTime   int64 `json:"created_time"`
}

// ResellerCustomer is one row of a reseller's customer list: the customer org
// plus how much the reseller has net-allocated to it (allocated − revoked).
type ResellerCustomer struct {
	Org          *Organization `json:"org"`
	NetAllocated int           `json:"net_allocated"`
}

// CreateResellerCustomer provisions a customer org (type enterprise, retail
// price group) and seeds it with an initial allocation from the reseller's
// wallet. The initial allocation both funds the customer and establishes the
// ledger relationship that makes it appear in the reseller's customer list.
// The customer starts ownerless (a reseller-managed shell); its owner is
// onboarded separately via the invitation flow.
func CreateResellerCustomer(resellerOrgId int, name, priceGroup string, initialQuota, operatorId int) (*Organization, error) {
	reseller, err := GetOrganizationById(resellerOrgId)
	if err != nil {
		return nil, err
	}
	if reseller == nil || reseller.Type != OrgTypeReseller {
		return nil, errors.New("只有代理商组织可以创建客户")
	}
	if initialQuota <= 0 {
		return nil, errors.New("初始划拨额度必须为正")
	}
	if reseller.WalletQuota < initialQuota {
		return nil, errors.New("代理商钱包余额不足")
	}
	if priceGroup == "" {
		priceGroup = "default"
	}
	customer := &Organization{Name: name, Type: OrgTypeEnterprise, PriceGroup: priceGroup}
	if err := CreateOrganization(customer); err != nil {
		return nil, err
	}
	// Record the authorization link before funding.
	if err := DB.Create(&ResellerCustomerLink{ResellerOrgId: resellerOrgId, CustomerOrgId: customer.Id, CreatedTime: common.GetTimestamp()}).Error; err != nil {
		DB.Delete(&Organization{}, customer.Id)
		return nil, err
	}
	// Fund it (atomic wallet move + ledger). On failure — e.g. a race drained
	// the reseller wallet after the pre-check — remove the orphan shell + link.
	if err := TransferOrgCredit(resellerOrgId, customer.Id, initialQuota, operatorId, LedgerTypeAllocate, "initial allocation"); err != nil {
		DB.Where("customer_org_id = ?", customer.Id).Delete(&ResellerCustomerLink{})
		DB.Delete(&Organization{}, customer.Id)
		return nil, err
	}
	// Re-fetch so the returned org reflects the funded wallet (TransferOrgCredit
	// updated the DB row, not the in-memory struct).
	if fresh, err := GetOrganizationById(customer.Id); err == nil && fresh != nil {
		customer = fresh
	}
	return customer, nil
}

// ListResellerCustomers returns the reseller's linked customers, each with the
// org and the current net allocation.
func ListResellerCustomers(resellerOrgId int) ([]*ResellerCustomer, error) {
	var links []ResellerCustomerLink
	if err := DB.Where("reseller_org_id = ?", resellerOrgId).Order("id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	out := make([]*ResellerCustomer, 0, len(links))
	for _, link := range links {
		org, err := GetOrganizationById(link.CustomerOrgId)
		if err != nil || org == nil {
			continue
		}
		net, err := NetAllocatedBetween(resellerOrgId, link.CustomerOrgId)
		if err != nil {
			return nil, err
		}
		out = append(out, &ResellerCustomer{Org: org, NetAllocated: net})
	}
	return out, nil
}

// IsResellerCustomer authorizes a reseller to fund/view a customer: true only
// when an explicit link exists. A ledger allocation alone does NOT grant
// access, so a reseller can never reach an org it did not provision (e.g. by
// pushing 1 quota at a stranger org to spy on its usage).
func IsResellerCustomer(resellerOrgId, customerOrgId int) (bool, error) {
	var count int64
	err := DB.Model(&ResellerCustomerLink{}).
		Where("reseller_org_id = ? AND customer_org_id = ?", resellerOrgId, customerOrgId).
		Count(&count).Error
	return count > 0, err
}

// IsCustomerOrg reports whether an org is any reseller's downstream customer
// (it appears as a customer in the link table). This is the marker that
// separates a reseller-provisioned customer from a B2B enterprise direct
// client, even though both are stored as enterprise-type organizations.
func IsCustomerOrg(orgId int) bool {
	var count int64
	if err := DB.Model(&ResellerCustomerLink{}).Where("customer_org_id = ?", orgId).Count(&count).Error; err != nil {
		return false
	}
	return count > 0
}

// CustomerOrgIdSet returns the set of all customer org ids (for admin list
// separation: enterprise-direct = enterprise orgs NOT in this set).
func CustomerOrgIdSet() (map[int]bool, error) {
	var ids []int
	if err := DB.Model(&ResellerCustomerLink{}).Pluck("customer_org_id", &ids).Error; err != nil {
		return nil, err
	}
	set := make(map[int]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set, nil
}
