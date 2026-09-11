// Fork-only organization system: enterprise orgs and resellers share one
// skeleton (design doc: "wrouter 组织与分销架构"). Upstream contact points are
// two AutoMigrate lines in model/main.go and the funding hook in
// service/billing_session.go — everything else lives in fork files.
//
// Billing invariants:
//   - The org wallet only changes through atomic conditional updates
//     (TryReserveOrgQuota) or row-locked ledger transactions.
//   - credit_ledger is append-only; every wallet movement writes one row.
//   - Payer resolution is single-hop by construction: Organization has no
//     parent id, and OrgAccount.UserId is UNIQUE, so a user has at most one
//     paying organization and organizations never chain.
package model

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

const (
	OrgTypeEnterprise = "enterprise"
	OrgTypeReseller   = "reseller"

	OrgStatusActive    = "active"
	OrgStatusSuspended = "suspended"

	OrgRelationMember   = "member"
	OrgRelationCustomer = "customer"

	OrgRoleOwner  = "owner"
	OrgRoleAdmin  = "admin"
	OrgRoleMember = "member"

	LedgerTypePurchase = "purchase"
	LedgerTypeAllocate = "allocate"
	LedgerTypeRevoke   = "revoke"
)

// Organization is the paying entity for managed accounts. Deliberately has NO
// parent_org_id: relationships between organizations exist only as ledger
// rows, which keeps request-time payer resolution single-hop.
type Organization struct {
	Id          int    `json:"id" gorm:"primarykey"`
	Name        string `json:"name" gorm:"type:varchar(128);not null"`
	Type        string `json:"type" gorm:"type:varchar(16);index;not null"` // enterprise | reseller
	Status      string `json:"status" gorm:"type:varchar(16);index"`        // active | suspended
	WalletQuota int    `json:"wallet_quota"`
	PriceGroup  string `json:"price_group" gorm:"type:varchar(64)"` // wholesale / negotiated group
	// WholesaleRatio is the reseller's wholesale price for buying wallet credit:
	// personal quota spent = purchased credit × ratio. 0 (or out of (0,1]) means
	// no discount (1.0). Admin-set per negotiated deal; it is the reseller's
	// margin lever (they resell that credit to customers at their own price).
	WholesaleRatio float64 `json:"wholesale_ratio"`
	// AllowedModels is a JSON array of model names this org may use. Empty =
	// unrestricted (whatever the org's group can route). On a RESELLER org it is
	// admin-set and bounds what the reseller may offer; on a CUSTOMER org it is
	// reseller-set and is the runtime allow-list enforced on that customer's
	// requests. See AllowedModelSet.
	AllowedModels string `json:"allowed_models" gorm:"type:text"`
	// RetailDiscounts is a reseller-set JSON map {model-series token -> ratio in
	// (0,1]} on a CUSTOMER org. It is a RETAIL/reporting overlay only (the
	// platform still bills the customer at standard price): the reseller's
	// customer statement multiplies standard cost by the matched ratio to get
	// what the customer owes the reseller. Never touches the billing hot path.
	RetailDiscounts string `json:"retail_discounts" gorm:"type:text"`
	OwnerUserId     int    `json:"owner_user_id" gorm:"index"`
	Remark          string `json:"remark" gorm:"type:varchar(255)"`
	CreatedTime     int64  `json:"created_time"`
	UpdatedTime     int64  `json:"updated_time"`
	// IsCustomer is a computed, non-persisted flag: true when this org is a
	// reseller-provisioned customer (in ResellerCustomerLink). Lets the admin UI
	// separate enterprise direct clients from reseller customers.
	IsCustomer bool `json:"is_customer" gorm:"-"`
}

// OrgAccount binds a user to the organization that pays for it. UserId is
// UNIQUE: an account has exactly one payer at any moment — ambiguity is
// eliminated at the schema level.
type OrgAccount struct {
	Id            int    `json:"id" gorm:"primarykey"`
	OrgId         int    `json:"org_id" gorm:"index;not null"`
	UserId        int    `json:"user_id" gorm:"uniqueIndex;not null"`
	Relation      string `json:"relation" gorm:"type:varchar(16)"` // member | customer
	Role          string `json:"role" gorm:"type:varchar(16)"`     // owner | admin | member
	MonthlyBudget int    `json:"monthly_budget"`                   // 0 = unlimited
	PeriodKey     string `json:"period_key" gorm:"type:varchar(8)"`
	PeriodSpend   int    `json:"period_spend"`
	RegisteredBy  string `json:"registered_by" gorm:"type:varchar(64)"` // deal registration
	Status        string `json:"status" gorm:"type:varchar(16);index"`  // active | suspended
	CreatedTime   int64  `json:"created_time"`
}

// CreditLedger is append-only: rows are never updated or deleted.
type CreditLedger struct {
	Id          int    `json:"id" gorm:"primarykey"`
	FromOrgId   int    `json:"from_org_id" gorm:"index"` // 0 = platform (purchase)
	ToOrgId     int    `json:"to_org_id" gorm:"index"`
	Quota       int    `json:"quota"`                        // always positive
	Type        string `json:"type" gorm:"type:varchar(16)"` // purchase | allocate | revoke
	OperatorId  int    `json:"operator_id"`                  // acting user
	TradeNo     string `json:"trade_no" gorm:"type:varchar(64);index"`
	Remark      string `json:"remark" gorm:"type:varchar(255)"`
	CreatedTime int64  `json:"created_time"`
}

// Workspace is a policy/budget container inside an organization. It is NOT a
// wallet: billing stays on the organization (unified billing, like
// OpenRouter); the workspace only carries a periodic budget and groups keys.
type Workspace struct {
	Id            int    `json:"id" gorm:"primarykey"`
	OrgId         int    `json:"org_id" gorm:"index;not null"`
	Name          string `json:"name" gorm:"type:varchar(128);not null"`
	Status        string `json:"status" gorm:"type:varchar(16)"` // active | suspended
	MonthlyBudget int    `json:"monthly_budget"`                 // 0 = unlimited
	PeriodKey     string `json:"period_key" gorm:"type:varchar(8)"`
	PeriodSpend   int    `json:"period_spend"`
	CreatedTime   int64  `json:"created_time"`
}

// WorkspaceToken binds an API token to a workspace (mapping table so the
// upstream tokens table stays untouched). TokenId is UNIQUE: a key lives in
// at most one workspace.
type WorkspaceToken struct {
	Id          int   `json:"id" gorm:"primarykey"`
	WorkspaceId int   `json:"workspace_id" gorm:"index;not null"`
	OrgId       int   `json:"org_id" gorm:"index;not null"`
	TokenId     int   `json:"token_id" gorm:"uniqueIndex;not null"`
	CreatedTime int64 `json:"created_time"`
}

// ---------------------------------------------------------------------------
// Hot-path lookup with a small TTL cache
// ---------------------------------------------------------------------------

// parseAllowedModels parses the JSON model-name array stored on an org. An
// empty/blank/invalid value yields nil = unrestricted.
func parseAllowedModels(s string) map[string]bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return nil
	}
	var list []string
	if err := common.Unmarshal([]byte(s), &list); err != nil {
		return nil
	}
	set := make(map[string]bool, len(list))
	for _, m := range list {
		if m = strings.TrimSpace(m); m != "" {
			set[m] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// AllowedModelSet returns the org's allow-list as a set, or nil if unrestricted.
func (org *Organization) AllowedModelSet() map[string]bool {
	return parseAllowedModels(org.AllowedModels)
}

// MarshalAllowedModels serializes a model-name list for storage, de-duplicated
// and trimmed. An empty result serializes to "" (= unrestricted).
func MarshalAllowedModels(models []string) (string, error) {
	cleaned := make([]string, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		if m = strings.TrimSpace(m); m != "" && !seen[m] {
			seen[m] = true
			cleaned = append(cleaned, m)
		}
	}
	if len(cleaned) == 0 {
		return "", nil
	}
	b, err := common.Marshal(cleaned)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// AllowedModelList returns the org's allow-list as an ordered slice (empty =
// unrestricted), for display/editing.
func (org *Organization) AllowedModelList() []string {
	s := strings.TrimSpace(org.AllowedModels)
	if s == "" {
		return []string{}
	}
	var list []string
	if err := common.Unmarshal([]byte(s), &list); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(list))
	for _, m := range list {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// SetOrgAllowedModels persists an org's model allow-list (already serialized via
// MarshalAllowedModels) and invalidates the payer cache for the org's members so
// the distributor enforcement converges immediately instead of after the TTL.
func SetOrgAllowedModels(orgId int, stored string) error {
	if err := DB.Model(&Organization{}).Where("id = ?", orgId).Update("AllowedModels", stored).Error; err != nil {
		return err
	}
	var userIds []int
	if err := DB.Model(&OrgAccount{}).Where("org_id = ?", orgId).Pluck("user_id", &userIds).Error; err == nil {
		for _, uid := range userIds {
			InvalidateOrgPayerCache(uid)
		}
	}
	return nil
}

// OrgPayerInfo is everything the billing path needs to charge an organization
// for a managed account's request.
type OrgPayerInfo struct {
	OrgId         int
	OrgStatus     string
	OrgType       string
	AccountStatus string
	MonthlyBudget int
	Relation      string
	// AllowedModels is the payer org's runtime model allow-list (nil =
	// unrestricted). Enforced in the distributor so a reseller-provisioned
	// customer can only use the models it was assigned.
	AllowedModels map[string]bool
}

type orgPayerCacheEntry struct {
	info      *OrgPayerInfo // nil = user is not managed
	expiresAt time.Time
}

var (
	orgPayerCache    sync.Map // userId -> orgPayerCacheEntry
	orgPayerCacheTTL = 30 * time.Second
)

// InvalidateOrgPayerCache must be called after attach/detach/suspend/budget
// changes so the hot path converges within one request instead of the TTL.
func InvalidateOrgPayerCache(userId int) {
	orgPayerCache.Delete(userId)
}

// GetOrgPayerInfo resolves the managing organization for a user, or nil when
// the user pays for itself. Single indexed lookup, TTL-cached.
func GetOrgPayerInfo(userId int) (*OrgPayerInfo, error) {
	if v, ok := orgPayerCache.Load(userId); ok {
		entry := v.(orgPayerCacheEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.info, nil
		}
	}

	var row struct {
		OrgId         int
		OrgStatus     string
		OrgType       string
		AccountStatus string
		MonthlyBudget int
		Relation      string
		AllowedModels string
	}
	err := DB.Table("org_accounts").
		Select("org_accounts.org_id as org_id, organizations.status as org_status, organizations.type as org_type, org_accounts.status as account_status, org_accounts.monthly_budget as monthly_budget, org_accounts.relation as relation, organizations.allowed_models as allowed_models").
		Joins("join organizations on organizations.id = org_accounts.org_id").
		Where("org_accounts.user_id = ?", userId).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	var info *OrgPayerInfo
	if row.OrgId != 0 {
		info = &OrgPayerInfo{
			OrgId:         row.OrgId,
			OrgStatus:     row.OrgStatus,
			OrgType:       row.OrgType,
			AccountStatus: row.AccountStatus,
			MonthlyBudget: row.MonthlyBudget,
			Relation:      row.Relation,
			AllowedModels: parseAllowedModels(row.AllowedModels),
		}
	}
	orgPayerCache.Store(userId, orgPayerCacheEntry{info: info, expiresAt: time.Now().Add(orgPayerCacheTTL)})
	return info, nil
}

// ---------------------------------------------------------------------------
// Atomic wallet operations (mirror the user-quota reserve pattern)
// ---------------------------------------------------------------------------

// TryReserveOrgQuota atomically deducts quota from the org wallet when the
// balance suffices. Returns (false, nil) on insufficient balance.
func TryReserveOrgQuota(orgId int, quota int) (bool, error) {
	if quota < 0 {
		return false, errors.New("quota 不能为负数")
	}
	if quota == 0 {
		return true, nil
	}
	result := DB.Model(&Organization{}).
		Where("id = ? AND status = ? AND wallet_quota >= ?", orgId, OrgStatusActive, quota).
		Updates(map[string]interface{}{
			"wallet_quota": gorm.Expr("wallet_quota - ?", quota),
			"updated_time": common.GetTimestamp(),
		})
	return result.RowsAffected == 1, result.Error
}

// IncreaseOrgQuota returns quota to the org wallet (refund / negative settle).
func IncreaseOrgQuota(orgId int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数")
	}
	if quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).Where("id = ?", orgId).
		Updates(map[string]interface{}{
			"wallet_quota": gorm.Expr("wallet_quota + ?", quota),
			"updated_time": common.GetTimestamp(),
		}).Error
}

// DecreaseOrgQuota deducts additional quota at settle time (positive delta).
// Unlike reserve it may drive the wallet negative: the tokens were already
// consumed upstream, so the debt must be recorded rather than dropped.
func DecreaseOrgQuota(orgId int, quota int) error {
	if quota < 0 {
		return errors.New("quota 不能为负数")
	}
	if quota == 0 {
		return nil
	}
	return DB.Model(&Organization{}).Where("id = ?", orgId).
		Updates(map[string]interface{}{
			"wallet_quota": gorm.Expr("wallet_quota - ?", quota),
			"updated_time": common.GetTimestamp(),
		}).Error
}

// ---------------------------------------------------------------------------
// Periodic budget counters (hard enforcement, cross-DB safe)
// ---------------------------------------------------------------------------

func currentPeriodKey() string {
	return time.Now().UTC().Format("200601")
}

// rollPeriodIfNeeded atomically resets the spend counter when the stored
// period differs from the current one. The guarded WHERE makes concurrent
// rollovers collapse into a single reset.
func rollPeriodIfNeeded(table string, where string, args []interface{}, storedKey string) error {
	period := currentPeriodKey()
	if storedKey == period {
		return nil
	}
	q := DB.Table(table).Where(where+" AND period_key = ?", append(append([]interface{}{}, args...), storedKey)...)
	return q.Updates(map[string]interface{}{"period_key": period, "period_spend": 0}).Error
}

// AddOrgAccountSpend adds quota to the member's monthly counter, enforcing the
// budget atomically (a concurrent burst cannot overshoot: the conditional
// UPDATE is the arbiter). budget==0 means unlimited. Returns false when the
// budget would be exceeded.
// enforceBudget=false is the settle-overshoot path: the upstream tokens were
// already consumed, so the counter must record the spend even past the budget
// (the budget then blocks FUTURE requests instead of dropping the debt).
func AddOrgAccountSpend(orgId, userId, quota int, enforceBudget bool) (bool, error) {
	if quota <= 0 {
		return true, nil
	}
	var acc OrgAccount
	if err := DB.Where("org_id = ? AND user_id = ?", orgId, userId).First(&acc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// Key-level billing: the token owner need not be an org member; when
			// there is no member row there is no per-seat cap to enforce.
			return true, nil
		}
		return false, err
	}
	if err := rollPeriodIfNeeded("org_accounts", "org_id = ? AND user_id = ?", []interface{}{orgId, userId}, acc.PeriodKey); err != nil {
		return false, err
	}
	period := currentPeriodKey()
	q := DB.Model(&OrgAccount{}).Where("org_id = ? AND user_id = ? AND period_key = ?", orgId, userId, period)
	if enforceBudget {
		q = q.Where("monthly_budget = 0 OR period_spend + ? <= monthly_budget", quota)
	}
	result := q.Update("period_spend", gorm.Expr("period_spend + ?", quota))
	return result.RowsAffected == 1, result.Error
}

// ReduceOrgAccountSpend returns quota to the member counter on refund or
// negative settle. Floors at zero (CASE keeps it cross-DB portable).
func ReduceOrgAccountSpend(orgId, userId, quota int) error {
	if quota <= 0 {
		return nil
	}
	return DB.Model(&OrgAccount{}).
		Where("org_id = ? AND user_id = ? AND period_key = ?", orgId, userId, currentPeriodKey()).
		Update("period_spend", gorm.Expr("CASE WHEN period_spend >= ? THEN period_spend - ? ELSE 0 END", quota, quota)).Error
}

// AddWorkspaceSpend / ReduceWorkspaceSpend mirror the account counters for
// the workspace the request's token belongs to.
func AddWorkspaceSpend(workspaceId, quota int, enforceBudget bool) (bool, error) {
	if quota <= 0 || workspaceId == 0 {
		return true, nil
	}
	var ws Workspace
	if err := DB.Where("id = ?", workspaceId).First(&ws).Error; err != nil {
		return false, err
	}
	if enforceBudget && ws.Status == OrgStatusSuspended {
		return false, nil
	}
	if err := rollPeriodIfNeeded("workspaces", "id = ?", []interface{}{workspaceId}, ws.PeriodKey); err != nil {
		return false, err
	}
	q := DB.Model(&Workspace{}).Where("id = ? AND period_key = ?", workspaceId, currentPeriodKey())
	if enforceBudget {
		q = q.Where("monthly_budget = 0 OR period_spend + ? <= monthly_budget", quota)
	}
	result := q.Update("period_spend", gorm.Expr("period_spend + ?", quota))
	return result.RowsAffected == 1, result.Error
}

func ReduceWorkspaceSpend(workspaceId, quota int) error {
	if quota <= 0 || workspaceId == 0 {
		return nil
	}
	return DB.Model(&Workspace{}).
		Where("id = ? AND period_key = ?", workspaceId, currentPeriodKey()).
		Update("period_spend", gorm.Expr("CASE WHEN period_spend >= ? THEN period_spend - ? ELSE 0 END", quota, quota)).Error
}

// GetTokenWorkspaceId returns the workspace a token is bound to (0 = none),
// TTL-cached for the hot path.
var tokenWorkspaceCache sync.Map // tokenId -> orgPayerCacheEntry-like

type tokenWorkspaceEntry struct {
	workspaceId int
	expiresAt   time.Time
}

func InvalidateTokenWorkspaceCache(tokenId int) {
	tokenWorkspaceCache.Delete(tokenId)
}

// WorkspaceBillingInfo is what the billing path needs when a request's TOKEN
// is bound to an organization workspace (OpenRouter-style key-level billing:
// a request bills the org only when its key lives in an org workspace; a
// personal/unbound key bills the user personally).
type WorkspaceBillingInfo struct {
	WorkspaceId     int
	OrgId           int
	OrgStatus       string
	WorkspaceStatus string
}

// GetWorkspaceBillingInfo resolves the org that pays for a token via its
// workspace binding, or nil when the token is not bound (personal billing).
// The token→workspace mapping is TTL-cached; org/workspace status is read
// live (two PK lookups) so suspension takes effect immediately.
func GetWorkspaceBillingInfo(tokenId int) (*WorkspaceBillingInfo, error) {
	wsId, err := GetTokenWorkspaceId(tokenId)
	if err != nil {
		return nil, err
	}
	if wsId == 0 {
		return nil, nil
	}
	ws, err := GetWorkspaceById(wsId)
	if err != nil {
		return nil, err
	}
	if ws == nil {
		// Stale binding (workspace deleted) — treat as unbound.
		return nil, nil
	}
	org, err := GetOrganizationById(ws.OrgId)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, nil
	}
	return &WorkspaceBillingInfo{
		WorkspaceId:     wsId,
		OrgId:           ws.OrgId,
		OrgStatus:       org.Status,
		WorkspaceStatus: ws.Status,
	}, nil
}

func GetTokenWorkspaceId(tokenId int) (int, error) {
	if tokenId == 0 {
		return 0, nil
	}
	if v, ok := tokenWorkspaceCache.Load(tokenId); ok {
		entry := v.(tokenWorkspaceEntry)
		if time.Now().Before(entry.expiresAt) {
			return entry.workspaceId, nil
		}
	}
	var wt WorkspaceToken
	err := DB.Where("token_id = ?", tokenId).Limit(1).Find(&wt).Error
	if err != nil {
		return 0, err
	}
	tokenWorkspaceCache.Store(tokenId, tokenWorkspaceEntry{workspaceId: wt.WorkspaceId, expiresAt: time.Now().Add(orgPayerCacheTTL)})
	return wt.WorkspaceId, nil
}

// ---------------------------------------------------------------------------
// Ledger transactions — the only ways money moves between org wallets
// ---------------------------------------------------------------------------

func insertLedger(tx *gorm.DB, fromOrg, toOrg, quota, operatorId int, ledgerType, tradeNo, remark string) error {
	return tx.Create(&CreditLedger{
		FromOrgId:   fromOrg,
		ToOrgId:     toOrg,
		Quota:       quota,
		Type:        ledgerType,
		OperatorId:  operatorId,
		TradeNo:     tradeNo,
		Remark:      remark,
		CreatedTime: common.GetTimestamp(),
	}).Error
}

// PlatformCreditOrg credits an org wallet from the platform (admin purchase /
// invoiced top-up). Appends a purchase ledger row in the same transaction.
func PlatformCreditOrg(orgId, quota, operatorId int, tradeNo, remark string) error {
	if quota <= 0 {
		return errors.New("credit quota must be positive")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := lockForUpdate(tx).Where("id = ?", orgId).First(&org).Error; err != nil {
			return errors.New("organization not found")
		}
		if err := tx.Model(&Organization{}).Where("id = ?", orgId).
			Updates(map[string]interface{}{
				"wallet_quota": gorm.Expr("wallet_quota + ?", quota),
				"updated_time": common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		return insertLedger(tx, 0, orgId, quota, operatorId, LedgerTypePurchase, tradeNo, remark)
	})
}

// TransferOrgCredit moves quota between two org wallets (allocate: reseller →
// nested customer org; revoke: the reverse, limited to the source's unconsumed
// balance by the conditional deduct). Both movements are the same primitive
// with different ledger types.
func TransferOrgCredit(fromOrgId, toOrgId, quota, operatorId int, ledgerType, remark string) error {
	if quota <= 0 {
		return errors.New("transfer quota must be positive")
	}
	if fromOrgId == toOrgId {
		return errors.New("cannot transfer to the same organization")
	}
	if ledgerType != LedgerTypeAllocate && ledgerType != LedgerTypeRevoke {
		return errors.New("invalid ledger type")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		// Deterministic lock order prevents deadlocks on crossing transfers.
		firstId, secondId := fromOrgId, toOrgId
		if secondId < firstId {
			firstId, secondId = secondId, firstId
		}
		var a, b Organization
		if err := lockForUpdate(tx).Where("id = ?", firstId).First(&a).Error; err != nil {
			return errors.New("organization not found")
		}
		if err := lockForUpdate(tx).Where("id = ?", secondId).First(&b).Error; err != nil {
			return errors.New("organization not found")
		}
		deduct := tx.Model(&Organization{}).
			Where("id = ? AND wallet_quota >= ?", fromOrgId, quota).
			Updates(map[string]interface{}{
				"wallet_quota": gorm.Expr("wallet_quota - ?", quota),
				"updated_time": common.GetTimestamp(),
			})
		if deduct.Error != nil {
			return deduct.Error
		}
		if deduct.RowsAffected != 1 {
			return fmt.Errorf("insufficient unconsumed balance in organization %d", fromOrgId)
		}
		if err := tx.Model(&Organization{}).Where("id = ?", toOrgId).
			Updates(map[string]interface{}{
				"wallet_quota": gorm.Expr("wallet_quota + ?", quota),
				"updated_time": common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		return insertLedger(tx, fromOrgId, toOrgId, quota, operatorId, ledgerType, "", remark)
	})
}

// ---------------------------------------------------------------------------
// CRUD & console queries
// ---------------------------------------------------------------------------

// ValidateOrgName enforces the org-name rules shared by self-serve and admin
// creation: at least 3 characters, at most 64, and no special characters —
// only letters (any script, including CJK), digits, spaces, hyphen and
// underscore. Callers should pass an already-trimmed name; it trims again to
// be safe.
func ValidateOrgName(name string) error {
	name = strings.TrimSpace(name)
	n := utf8.RuneCountInString(name)
	if n < 3 {
		return errors.New("组织名称至少 3 个字符")
	}
	if n > 64 {
		return errors.New("组织名称过长（最多 64 个字符）")
	}
	for _, r := range name {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == ' ' || r == '-' || r == '_' {
			continue
		}
		return errors.New("组织名称不支持特殊字符")
	}
	return nil
}

func CreateOrganization(org *Organization) error {
	org.Name = strings.TrimSpace(org.Name)
	if err := ValidateOrgName(org.Name); err != nil {
		return err
	}
	if org.Type != OrgTypeEnterprise && org.Type != OrgTypeReseller {
		return errors.New("invalid organization type")
	}
	org.Status = OrgStatusActive
	org.WalletQuota = 0
	org.CreatedTime = common.GetTimestamp()
	org.UpdatedTime = org.CreatedTime
	return DB.Create(org).Error
}

func GetOrganizationById(id int) (*Organization, error) {
	var org Organization
	err := DB.Where("id = ?", id).First(&org).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &org, err
}

// ListOrganizations lists orgs, optionally narrowed by category: "customer"
// (reseller-provisioned, in the link table), "enterprise" (direct clients, NOT
// in the link table), or "" / "all" (everything). This is the hard separation
// between the two user groups in the admin UI.
func ListOrganizations(offset, limit int, category string) ([]*Organization, int64, error) {
	q := DB.Model(&Organization{})
	sub := DB.Model(&ResellerCustomerLink{}).Select("customer_org_id")
	switch category {
	case "customer":
		q = q.Where("id IN (?)", sub)
	case "enterprise":
		q = q.Where("id NOT IN (?)", sub)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orgs []*Organization
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&orgs).Error
	return orgs, total, err
}

func UpdateOrganizationFields(id int, fields map[string]interface{}) error {
	fields["updated_time"] = common.GetTimestamp()
	return DB.Model(&Organization{}).Where("id = ?", id).Updates(fields).Error
}

// AttachOrgAccount binds a user to an organization. Fails when the user is
// already managed anywhere (UNIQUE user_id) — detach first, explicitly.
func AttachOrgAccount(acc *OrgAccount) error {
	if acc.Relation != OrgRelationMember && acc.Relation != OrgRelationCustomer {
		return errors.New("invalid relation")
	}
	if acc.Role != OrgRoleOwner && acc.Role != OrgRoleAdmin && acc.Role != OrgRoleMember {
		return errors.New("invalid role")
	}
	acc.Status = OrgStatusActive
	acc.PeriodKey = currentPeriodKey()
	acc.PeriodSpend = 0
	acc.CreatedTime = common.GetTimestamp()
	err := DB.Create(acc).Error
	if err == nil {
		InvalidateOrgPayerCache(acc.UserId)
	}
	return err
}

func DetachOrgAccount(orgId, userId int) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("org_id = ? AND user_id = ?", orgId, userId).Delete(&OrgAccount{}).Error; err != nil {
			return err
		}
		// Clear the user's workspace bindings so a later re-attach to another
		// org can't be gated by / pollute this org's workspace budget.
		var tokenIds []int
		if err := tx.Model(&Token{}).Where("user_id = ?", userId).Pluck("id", &tokenIds).Error; err != nil {
			return err
		}
		if len(tokenIds) > 0 {
			if err := tx.Where("token_id IN ?", tokenIds).Delete(&WorkspaceToken{}).Error; err != nil {
				return err
			}
			for _, tid := range tokenIds {
				InvalidateTokenWorkspaceCache(tid)
			}
		}
		return nil
	})
	if err == nil {
		InvalidateOrgPayerCache(userId)
	}
	return err
}

func UpdateOrgAccountFields(orgId, userId int, fields map[string]interface{}) error {
	err := DB.Model(&OrgAccount{}).Where("org_id = ? AND user_id = ?", orgId, userId).Updates(fields).Error
	if err == nil {
		InvalidateOrgPayerCache(userId)
	}
	return err
}

func ListOrgAccounts(orgId int) ([]*OrgAccount, error) {
	var accounts []*OrgAccount
	err := DB.Where("org_id = ?", orgId).Order("id ASC").Find(&accounts).Error
	return accounts, err
}

// GetOrgAccountByUser returns the caller's own org binding (for console auth).
func GetOrgAccountByUser(userId int) (*OrgAccount, error) {
	var acc OrgAccount
	err := DB.Where("user_id = ?", userId).First(&acc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &acc, err
}

// NetAllocatedBetween returns how much credit fromOrg has allocated to toOrg
// net of what it has already revoked. This is the authorization bound for a
// revoke: without a parent link, a reseller may only pull back what it put in.
func NetAllocatedBetween(fromOrgId, toOrgId int) (int, error) {
	type sumRow struct{ Total int64 }
	var allocated, revoked sumRow
	if err := DB.Model(&CreditLedger{}).
		Select("COALESCE(SUM(quota),0) as total").
		Where("from_org_id = ? AND to_org_id = ? AND type = ?", fromOrgId, toOrgId, LedgerTypeAllocate).
		Scan(&allocated).Error; err != nil {
		return 0, err
	}
	if err := DB.Model(&CreditLedger{}).
		Select("COALESCE(SUM(quota),0) as total").
		Where("from_org_id = ? AND to_org_id = ? AND type = ?", toOrgId, fromOrgId, LedgerTypeRevoke).
		Scan(&revoked).Error; err != nil {
		return 0, err
	}
	return int(allocated.Total - revoked.Total), nil
}

func ListOrgLedger(orgId int, offset, limit int) ([]*CreditLedger, int64, error) {
	var rows []*CreditLedger
	var total int64
	q := DB.Model(&CreditLedger{}).Where("from_org_id = ? OR to_org_id = ?", orgId, orgId)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("id DESC").Offset(offset).Limit(limit).Find(&rows).Error
	return rows, total, err
}

// ---------------------------------------------------------------------------
// Workspace CRUD
// ---------------------------------------------------------------------------

func CreateWorkspace(ws *Workspace) error {
	ws.Name = strings.TrimSpace(ws.Name)
	// Workspace names follow the same rule as org names (>= 3 chars, no
	// special characters) for a consistent, injection-safe identifier.
	if err := ValidateOrgName(ws.Name); err != nil {
		return err
	}
	ws.Status = OrgStatusActive
	ws.PeriodKey = currentPeriodKey()
	ws.PeriodSpend = 0
	ws.CreatedTime = common.GetTimestamp()
	return DB.Create(ws).Error
}

func ListWorkspaces(orgId int) ([]*Workspace, error) {
	var out []*Workspace
	err := DB.Where("org_id = ?", orgId).Order("id ASC").Find(&out).Error
	return out, err
}

func GetWorkspaceById(id int) (*Workspace, error) {
	var ws Workspace
	err := DB.Where("id = ?", id).First(&ws).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &ws, err
}

func UpdateWorkspaceFields(id int, fields map[string]interface{}) error {
	return DB.Model(&Workspace{}).Where("id = ?", id).Updates(fields).Error
}

func DeleteWorkspace(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var bindings []WorkspaceToken
		if err := tx.Where("workspace_id = ?", id).Find(&bindings).Error; err != nil {
			return err
		}
		if err := tx.Where("workspace_id = ?", id).Delete(&WorkspaceToken{}).Error; err != nil {
			return err
		}
		for _, b := range bindings {
			InvalidateTokenWorkspaceCache(b.TokenId)
		}
		return tx.Where("id = ?", id).Delete(&Workspace{}).Error
	})
}

// BindTokenToWorkspace attaches a token; rebinding moves it (upsert-ish via
// delete+create inside a transaction to stay cross-DB portable).
func BindTokenToWorkspace(orgId, workspaceId, tokenId int) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("token_id = ?", tokenId).Delete(&WorkspaceToken{}).Error; err != nil {
			return err
		}
		return tx.Create(&WorkspaceToken{
			OrgId:       orgId,
			WorkspaceId: workspaceId,
			TokenId:     tokenId,
			CreatedTime: common.GetTimestamp(),
		}).Error
	})
	if err == nil {
		InvalidateTokenWorkspaceCache(tokenId)
	}
	return err
}

func UnbindTokenFromWorkspace(tokenId int) error {
	err := DB.Where("token_id = ?", tokenId).Delete(&WorkspaceToken{}).Error
	if err == nil {
		InvalidateTokenWorkspaceCache(tokenId)
	}
	return err
}

func ListWorkspaceTokens(workspaceId int) ([]*WorkspaceToken, error) {
	var out []*WorkspaceToken
	err := DB.Where("workspace_id = ?", workspaceId).Find(&out).Error
	return out, err
}
