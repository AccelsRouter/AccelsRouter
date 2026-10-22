package model

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Business development (业务信息汇总): supplier / customer leads and the deals
// closed with them. Purchases and sales are recorded independently — there is
// no one-to-one matching between a purchase and a sale. Own tables only; no
// existing table is touched.

const (
	BdSideBuy  = "buy"  // 采购：能给我们供货的供应商，编号 B-001 起
	BdSideSell = "sell" // 销售：要买的客户，编号 S-001 起
)

// Lead stages, in pipeline order. "hold" (搁置) is excluded from matching.
var BdStages = []string{"contact", "confirm", "quote", "poc", "signing", "won", "hold"}

var BdPriorities = []string{"P0", "P1", "P2"}

var bdCurrencies = []string{"USD", "USDT/USDC", "CNY", "HKD", "SGD"}

var (
	ErrBdNotFound     = errors.New("record not found")
	ErrBdInvalidInput = errors.New("invalid input")
)

func bdInvalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBdInvalidInput, fmt.Sprintf(format, args...))
}

// BdCompany gives each supplier / customer one stable code. A company that
// gets a second lead keeps its existing code. The unique indexes make code
// allocation safe across instances: a collision fails the insert and the
// caller retries with the next sequence number.
type BdCompany struct {
	Id      int    `json:"id" gorm:"primaryKey"`
	Side    string `json:"side" gorm:"size:8;not null;uniqueIndex:uk_bd_company_name,priority:1;uniqueIndex:uk_bd_company_seq,priority:1"`
	NameKey string `json:"-" gorm:"size:128;not null;uniqueIndex:uk_bd_company_name,priority:2"`
	Name    string `json:"name" gorm:"size:128;not null"`
	Seq     int    `json:"seq" gorm:"not null;uniqueIndex:uk_bd_company_seq,priority:2"`
	Code    string `json:"code" gorm:"size:16;not null"`
	// Platform usage bindings, shared by every lead of the company: a
	// supplier maps to AccelsRouter channels, a customer to platform users
	// and/or organizations (all their member accounts). Comma-separated ids.
	ChannelIdList string `json:"-" gorm:"column:channel_ids;type:text"`
	UserIdList    string `json:"-" gorm:"column:user_ids;type:text"`
	OrgIdList     string `json:"-" gorm:"column:org_ids;type:text"`
	ChannelIds    []int  `json:"channel_ids" gorm:"-"`
	UserIds       []int  `json:"user_ids" gorm:"-"`
	OrgIds        []int  `json:"org_ids" gorm:"-"`
	CreatedAt     int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (BdCompany) TableName() string { return "bd_companies" }

func (c *BdCompany) AfterFind(*gorm.DB) error {
	c.ChannelIds = splitBdIds(c.ChannelIdList)
	c.UserIds = splitBdIds(c.UserIdList)
	c.OrgIds = splitBdIds(c.OrgIdList)
	return nil
}

// BdBinding is the usage binding sent with a lead; nil keeps the company's
// current binding.
type BdBinding struct {
	ChannelIds []int `json:"channel_ids"`
	UserIds    []int `json:"user_ids"`
	OrgIds     []int `json:"org_ids"`
}

type BdLead struct {
	Id         int      `json:"id" gorm:"primaryKey"`
	Side       string   `json:"side" gorm:"size:8;not null;index"`
	Code       string   `json:"code" gorm:"size:16;index"`
	Company    string   `json:"company" gorm:"size:128;not null;index"`
	Model      string   `json:"model" gorm:"size:128;not null;index"`
	ModelClass string   `json:"model_class" gorm:"size:64"`
	ModelNote  string   `json:"model_note" gorm:"size:255"` // buy side: 型号、限速、上下文等
	Volume     *float64 `json:"volume"`                     // sell side: 月量级 USD/月
	// Buy side: 每月预付金额 when the terms are prepaid, otherwise 每月后付额度.
	MonthlyAmount *float64   `json:"monthly_amount"`
	PriceRate     *float64   `json:"price_rate"` // % of official list price
	Currency      string     `json:"-" gorm:"column:currency;size:128"`
	Currencies    []string   `json:"currency" gorm:"-"`
	Contact       string     `json:"contact" gorm:"size:128"`
	ContactWay    string     `json:"contact_way" gorm:"size:255"`
	Region        string     `json:"region" gorm:"size:128"`
	Terms         string     `json:"terms" gorm:"size:64"`
	Stage         string     `json:"stage" gorm:"size:16;index"`
	Priority      string     `json:"priority" gorm:"size:4"`
	OwnerId       int        `json:"owner_id" gorm:"index"`
	OwnerName     string     `json:"owner_name" gorm:"size:64"`
	Note          string     `json:"note" gorm:"type:text"`
	CreatedBy     int        `json:"created_by" gorm:"index"`
	UpdatedBy     int        `json:"updated_by"`
	CreatedAt     int64      `json:"created_at"`
	UpdatedAt     int64      `json:"updated_at"`
	Bind          *BdBinding `json:"bind,omitempty" gorm:"-"`
}

func (BdLead) TableName() string { return "bd_leads" }

func (l *BdLead) AfterFind(*gorm.DB) error {
	l.Currencies = splitBdList(l.Currency)
	return nil
}

type BdDeal struct {
	Id         int      `json:"id" gorm:"primaryKey"`
	Kind       string   `json:"kind" gorm:"size:8;not null;index"` // buy = 采购记录, sell = 销售记录
	LeadId     int      `json:"lead_id" gorm:"index"`
	Company    string   `json:"company" gorm:"size:128;not null;index"`
	Model      string   `json:"model" gorm:"size:128"`
	ModelClass string   `json:"model_class" gorm:"size:64"`
	Rate       *float64 `json:"rate"` // buy: 供货价 %, sell: 售价 %
	Terms      string   `json:"terms" gorm:"size:64"`
	Amount     *float64 `json:"amount"` // 合同金额 USD, required when prepaid
	DealDate   string   `json:"deal_date" gorm:"size:10;index"`
	OwnerId    int      `json:"owner_id" gorm:"index"`
	OwnerName  string   `json:"owner_name" gorm:"size:64"`
	// Sell deals only, maintained by bd.manage holders.
	Supplier   string   `json:"supplier" gorm:"size:128;index"`
	SupplyRate *float64 `json:"supply_rate"`
	Note       string   `json:"note" gorm:"type:text"`
	CreatedBy  int      `json:"created_by"`
	UpdatedBy  int      `json:"updated_by"`
	CreatedAt  int64    `json:"created_at"`
	UpdatedAt  int64    `json:"updated_at"`
}

func (BdDeal) TableName() string { return "bd_deals" }

// ---------- normalization ----------

var bdSpaces = regexp.MustCompile(`\s+`)

// NormalizeBdModel turns "Claude-Sonnet-4.6" / "claude.sonnet_4.6" into
// "claude sonnet 4.6": lowercase, "-" and "_" become spaces, and a dot is kept
// only between two digits (a version number).
func NormalizeBdModel(s string) string {
	r := []rune(strings.ToLower(strings.TrimSpace(s)))
	isDigit := func(i int) bool { return i >= 0 && i < len(r) && r[i] >= '0' && r[i] <= '9' }
	var b strings.Builder
	for i, c := range r {
		switch {
		case c == '-' || c == '_':
			b.WriteRune(' ')
		case c == '.' && !(isDigit(i-1) && isDigit(i+1)):
			b.WriteRune(' ')
		default:
			b.WriteRune(c)
		}
	}
	return strings.TrimSpace(bdSpaces.ReplaceAllString(b.String(), " "))
}

func bdNameKey(s string) string {
	return strings.ToLower(strings.TrimSpace(bdSpaces.ReplaceAllString(s, " ")))
}

// IsBdPrepaid reports whether payment terms mean prepayment (预付费).
func IsBdPrepaid(terms string) bool {
	t := strings.ToLower(terms)
	return strings.Contains(t, "预付") || strings.Contains(t, "預付") || strings.Contains(t, "prepa")
}

func splitBdList(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func splitBdIds(s string) []int {
	out := []int{}
	for _, v := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// joinBdIds de-duplicates, sorts and joins positive ids.
func joinBdIds(ids []int) string {
	seen := map[int]bool{}
	clean := make([]int, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	sort.Ints(clean)
	parts := make([]string, len(clean))
	for i, id := range clean {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func bdClip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func bdContains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func bdValidRate(v *float64) bool { return v == nil || (*v >= 0 && *v <= 200) }

func validBdDate(s string) bool {
	if s == "" {
		return true
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// ---------- company codes ----------

func bdCodePrefix(side string) string {
	if side == BdSideBuy {
		return "B"
	}
	return "S"
}

// EnsureBdCompany returns a company, allocating the next code for its side on
// first use.
func EnsureBdCompany(tx *gorm.DB, side, name string) (*BdCompany, error) {
	key := bdNameKey(name)
	var existing BdCompany
	for attempt := 0; attempt < 5; attempt++ {
		err := tx.Where("side = ? AND name_key = ?", side, key).First(&existing).Error
		if err == nil {
			return &existing, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		var maxSeq int
		if err := tx.Model(&BdCompany{}).Where("side = ?", side).Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
			return nil, err
		}
		row := BdCompany{Side: side, NameKey: key, Name: strings.TrimSpace(name), Seq: maxSeq + 1}
		row.Code = fmt.Sprintf("%s-%03d", bdCodePrefix(side), row.Seq)
		// A concurrent insert of the same name or sequence violates a unique
		// index. Roll back to the savepoint (PostgreSQL aborts the whole
		// transaction on an error otherwise), then re-read and retry.
		sp := fmt.Sprintf("bd_company_%d", attempt)
		if err := tx.SavePoint(sp).Error; err != nil {
			return nil, err
		}
		if err := tx.Create(&row).Error; err == nil {
			return &row, nil
		}
		if err := tx.RollbackTo(sp).Error; err != nil {
			return nil, err
		}
	}
	return nil, errors.New("failed to allocate company code, please retry")
}

// ListBdCompanies returns every coded supplier / customer, for labelling
// deals and suggesting names.
func ListBdCompanies() ([]BdCompany, error) {
	var rows []BdCompany
	err := DB.Order("side, seq").Find(&rows).Error
	return rows, err
}

// ---------- leads ----------

func normalizeBdLead(l *BdLead) error {
	if l.Side != BdSideBuy && l.Side != BdSideSell {
		return bdInvalid("side must be buy or sell")
	}
	l.Company = bdClip(l.Company, 128)
	l.Model = bdClip(NormalizeBdModel(l.Model), 128)
	if l.Company == "" {
		return bdInvalid("company is required")
	}
	if l.Model == "" {
		return bdInvalid("model is required")
	}
	if l.Stage == "" {
		l.Stage = "contact"
	}
	if !bdContains(BdStages, l.Stage) {
		return bdInvalid("unknown stage %q", l.Stage)
	}
	if l.Priority == "" {
		l.Priority = "P1"
	}
	if !bdContains(BdPriorities, l.Priority) {
		return bdInvalid("unknown priority %q", l.Priority)
	}
	if !bdValidRate(l.PriceRate) {
		return bdInvalid("price rate must be between 0 and 200")
	}
	if l.Volume != nil && *l.Volume < 0 {
		return bdInvalid("volume must not be negative")
	}
	if l.MonthlyAmount != nil && *l.MonthlyAmount < 0 {
		return bdInvalid("monthly amount must not be negative")
	}
	currencies := make([]string, 0, len(l.Currencies))
	for _, c := range l.Currencies {
		if bdContains(bdCurrencies, c) && !bdContains(currencies, c) {
			currencies = append(currencies, c)
		}
	}
	l.Currencies = currencies
	l.Currency = strings.Join(currencies, ",")
	if l.Side == BdSideBuy {
		l.Volume = nil
	} else {
		l.ModelNote = ""
		l.MonthlyAmount = nil
	}
	l.ModelClass = bdClip(l.ModelClass, 64)
	l.ModelNote = bdClip(l.ModelNote, 255)
	l.Contact = bdClip(l.Contact, 128)
	l.ContactWay = bdClip(l.ContactWay, 255)
	l.Region = bdClip(l.Region, 128)
	l.Terms = bdClip(l.Terms, 64)
	l.Note = bdClip(l.Note, 4000)
	return nil
}

func ListBdLeads() ([]BdLead, error) {
	var rows []BdLead
	err := DB.Order("updated_at DESC, id DESC").Find(&rows).Error
	return rows, err
}

func GetBdLead(id int) (*BdLead, error) {
	var row BdLead
	err := DB.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBdNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveBdLead creates (Id == 0) or updates a lead. CreatedBy/CreatedAt of an
// existing lead are preserved.
func SaveBdLead(l *BdLead, operatorId int) error {
	if err := normalizeBdLead(l); err != nil {
		return err
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		company, err := EnsureBdCompany(tx, l.Side, l.Company)
		if err != nil {
			return err
		}
		l.Code = company.Code
		if l.Bind != nil {
			if err := setBdCompanyBinding(tx, company, l.Side, l.Bind); err != nil {
				return err
			}
		}
		l.UpdatedAt = now
		l.UpdatedBy = operatorId
		if l.Id == 0 {
			l.CreatedAt = now
			l.CreatedBy = operatorId
			return tx.Create(l).Error
		}
		var old BdLead
		if err := tx.First(&old, l.Id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBdNotFound
			}
			return err
		}
		l.Side = old.Side // a lead never changes side
		l.CreatedAt = old.CreatedAt
		l.CreatedBy = old.CreatedBy
		return tx.Model(&BdLead{Id: l.Id}).Select("*").Omit("id").Updates(l).Error
	})
}

func DeleteBdLead(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		res := tx.Delete(&BdLead{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBdNotFound
		}
		// Deals keep their own copy of company/model; only drop the link.
		return tx.Model(&BdDeal{}).Where("lead_id = ?", id).Update("lead_id", 0).Error
	})
}

// ---------- deals ----------

func normalizeBdDeal(d *BdDeal) error {
	if d.Kind != BdSideBuy && d.Kind != BdSideSell {
		return bdInvalid("kind must be buy or sell")
	}
	d.Company = bdClip(d.Company, 128)
	if d.Company == "" {
		return bdInvalid("company is required")
	}
	d.Model = bdClip(NormalizeBdModel(d.Model), 128)
	d.ModelClass = bdClip(d.ModelClass, 64)
	d.Terms = bdClip(d.Terms, 64)
	d.Note = bdClip(d.Note, 4000)
	d.Supplier = bdClip(d.Supplier, 128)
	if d.Kind == BdSideBuy && d.Rate == nil {
		return bdInvalid("supply price is required for a purchase record")
	}
	if !bdValidRate(d.Rate) || !bdValidRate(d.SupplyRate) {
		return bdInvalid("price rate must be between 0 and 200")
	}
	if d.Amount != nil && *d.Amount < 0 {
		return bdInvalid("contract amount must not be negative")
	}
	if IsBdPrepaid(d.Terms) && d.Amount == nil {
		return bdInvalid("contract amount is required for prepaid deals")
	}
	if !validBdDate(d.DealDate) {
		return bdInvalid("deal date must be YYYY-MM-DD")
	}
	if d.Kind == BdSideBuy {
		d.Supplier = ""
		d.SupplyRate = nil
	}
	return nil
}

// bdLinkLead finds the lead a deal belongs to: same company and model first,
// then any lead of that company.
func bdLinkLead(tx *gorm.DB, d *BdDeal) {
	if d.LeadId > 0 {
		var l BdLead
		if tx.Select("id, side, company").First(&l, d.LeadId).Error == nil &&
			l.Side == d.Kind && bdNameKey(l.Company) == bdNameKey(d.Company) {
			return
		}
	}
	d.LeadId = 0
	var leads []BdLead
	if tx.Select("id, company, model").Where("side = ?", d.Kind).Find(&leads).Error != nil {
		return
	}
	for _, l := range leads {
		if bdNameKey(l.Company) == bdNameKey(d.Company) {
			if l.Model == d.Model {
				d.LeadId = l.Id
				return
			}
			if d.LeadId == 0 {
				d.LeadId = l.Id
			}
		}
	}
}

func ListBdDeals() ([]BdDeal, error) {
	var rows []BdDeal
	err := DB.Order("deal_date DESC, id DESC").Find(&rows).Error
	return rows, err
}

func GetBdDeal(id int) (*BdDeal, error) {
	var row BdDeal
	err := DB.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBdNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveBdDeal creates (Id == 0) or updates a deal. When canManage is false the
// supplier fields of a sell deal are kept as stored (new deals get none).
func SaveBdDeal(d *BdDeal, operatorId int, canManage bool) error {
	if err := normalizeBdDeal(d); err != nil {
		return err
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := EnsureBdCompany(tx, d.Kind, d.Company); err != nil {
			return err
		}
		if d.Kind == BdSideSell && canManage && d.Supplier != "" {
			if _, err := EnsureBdCompany(tx, BdSideBuy, d.Supplier); err != nil {
				return err
			}
		}
		bdLinkLead(tx, d)
		d.UpdatedAt = now
		d.UpdatedBy = operatorId
		if d.Id == 0 {
			if !canManage {
				d.Supplier, d.SupplyRate = "", nil
			}
			d.CreatedAt = now
			d.CreatedBy = operatorId
			return tx.Create(d).Error
		}
		var old BdDeal
		if err := tx.First(&old, d.Id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBdNotFound
			}
			return err
		}
		d.Kind = old.Kind
		if !canManage {
			d.Supplier, d.SupplyRate = old.Supplier, old.SupplyRate
		}
		d.CreatedAt = old.CreatedAt
		d.CreatedBy = old.CreatedBy
		return tx.Model(&BdDeal{Id: d.Id}).Select("*").Omit("id").Updates(d).Error
	})
}

func DeleteBdDeal(id int) error {
	res := DB.Delete(&BdDeal{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrBdNotFound
	}
	return nil
}

// ---------- owners ----------

type BdOwner struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// ListBdOwnerCandidates returns enabled admin / root users; the controller
// narrows it to those who can read the BD module.
func ListBdOwnerCandidates() ([]User, error) {
	var users []User
	err := DB.Select("id, username, display_name, role").
		Where("role >= ? AND status = ?", common.RoleAdminUser, common.UserStatusEnabled).
		Order("id").Find(&users).Error
	return users, err
}

func BdUserName(u *User) string {
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Username
}

// ListBdModelNames returns the platform's model names, normalized the BD way,
// for input suggestions.
func ListBdModelNames() []string {
	var names []string
	if DB.Model(&Model{}).Pluck("model_name", &names).Error != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if v := NormalizeBdModel(n); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

// ---------- platform usage ----------

// setBdCompanyBinding stores a company's usage binding. Suppliers keep only
// channels, customers only users and organizations.
func setBdCompanyBinding(tx *gorm.DB, c *BdCompany, side string, b *BdBinding) error {
	fields := map[string]any{"channel_ids": "", "user_ids": "", "org_ids": ""}
	if side == BdSideBuy {
		fields["channel_ids"] = joinBdIds(b.ChannelIds)
	} else {
		fields["user_ids"] = joinBdIds(b.UserIds)
		fields["org_ids"] = joinBdIds(b.OrgIds)
	}
	return tx.Model(&BdCompany{}).Where("id = ?", c.Id).Updates(fields).Error
}

// BdUsage is the platform consumption of bound suppliers / customers over a
// window, in USD at the platform's list-price billing (quota_data quota /
// QuotaPerUnit, the same "standard" figure as reconciliation).
type BdUsage struct {
	From      int64           `json:"from"`
	To        int64           `json:"to"`
	Companies map[int]float64 `json:"companies"` // company id → USD
	BuyTotal  float64         `json:"buy_total"` // distinct bound channels
	SellTotal float64         `json:"sell_total"`
}

func bdQuotaBy(column string, ids []int, from, to int64) (map[int]int64, error) {
	out := map[int]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	type row struct {
		Dim   int
		Quota int64
	}
	var rows []row
	// column is one of two trusted names, never user input.
	err := DB.Table("quota_data").
		Select(column+" as dim, COALESCE(SUM(quota),0) as quota").
		Where(column+" IN ? AND created_at >= ? AND created_at < ?", ids, from, to).
		Group(column).Scan(&rows).Error
	for _, r := range rows {
		out[r.Dim] = r.Quota
	}
	return out, err
}

func bdKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// GetBdUsage sums quota_data over [from, to) per bound company. An
// organization counts every account of that org (members and customers).
func GetBdUsage(from, to int64) (*BdUsage, error) {
	companies, err := ListBdCompanies()
	if err != nil {
		return nil, err
	}
	allChannels, allOrgs := map[int]bool{}, map[int]bool{}
	for _, c := range companies {
		for _, id := range c.ChannelIds {
			allChannels[id] = true
		}
		for _, id := range c.OrgIds {
			allOrgs[id] = true
		}
	}
	orgUsers := map[int][]int{}
	if len(allOrgs) > 0 {
		var accounts []OrgAccount
		if err := DB.Select("org_id, user_id").Where("org_id IN ?", bdKeys(allOrgs)).Find(&accounts).Error; err != nil {
			return nil, err
		}
		for _, a := range accounts {
			orgUsers[a.OrgId] = append(orgUsers[a.OrgId], a.UserId)
		}
	}
	companyUsers := map[int]map[int]bool{}
	allUsers := map[int]bool{}
	for _, c := range companies {
		if c.Side != BdSideSell {
			continue
		}
		set := map[int]bool{}
		for _, id := range c.UserIds {
			set[id] = true
		}
		for _, org := range c.OrgIds {
			for _, id := range orgUsers[org] {
				set[id] = true
			}
		}
		companyUsers[c.Id] = set
		for id := range set {
			allUsers[id] = true
		}
	}

	byChannel, err := bdQuotaBy("channel_id", bdKeys(allChannels), from, to)
	if err != nil {
		return nil, err
	}
	byUser, err := bdQuotaBy("user_id", bdKeys(allUsers), from, to)
	if err != nil {
		return nil, err
	}

	usd := func(q int64) float64 { return float64(q) / common.QuotaPerUnit }
	res := &BdUsage{From: from, To: to, Companies: map[int]float64{}}
	countedChannels := map[int]bool{}
	for _, c := range companies {
		var q int64
		if c.Side == BdSideBuy {
			for _, id := range c.ChannelIds {
				q += byChannel[id]
				if !countedChannels[id] {
					countedChannels[id] = true
					res.BuyTotal += usd(byChannel[id])
				}
			}
		} else {
			for id := range companyUsers[c.Id] {
				q += byUser[id]
			}
		}
		if len(c.ChannelIds)+len(c.UserIds)+len(c.OrgIds) > 0 {
			res.Companies[c.Id] = usd(q)
		}
	}
	for id := range allUsers {
		res.SellTotal += usd(byUser[id])
	}
	return res, nil
}

// ---------- binding pickers ----------

type BdPick struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
}

func ListBdChannelPicks() ([]BdPick, error) {
	var rows []Channel
	if err := DB.Select("id, name, status").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]BdPick, 0, len(rows))
	for _, r := range rows {
		detail := ""
		if r.Status != common.ChannelStatusEnabled {
			detail = "disabled"
		}
		out = append(out, BdPick{Id: r.Id, Name: r.Name, Detail: detail})
	}
	return out, nil
}

func ListBdOrgPicks() ([]BdPick, error) {
	var rows []Organization
	if err := DB.Select("id, name, type").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]BdPick, 0, len(rows))
	for _, r := range rows {
		out = append(out, BdPick{Id: r.Id, Name: r.Name, Detail: r.Type})
	}
	return out, nil
}

func bdUserPick(u *User) BdPick {
	name := BdUserName(u)
	detail := ""
	if u.DisplayName != "" && u.DisplayName != u.Username {
		detail = u.Username
	}
	return BdPick{Id: u.Id, Name: name, Detail: detail}
}

// SearchBdUserPicks finds platform users by id, username or display name.
func SearchBdUserPicks(q string, limit int) ([]BdPick, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []BdPick{}, nil
	}
	tx := DB.Select("id, username, display_name").Limit(limit).Order("id")
	like := "%" + q + "%"
	if id, err := strconv.Atoi(q); err == nil {
		tx = tx.Where("id = ? OR username LIKE ? OR display_name LIKE ?", id, like, like)
	} else {
		tx = tx.Where("username LIKE ? OR display_name LIKE ?", like, like)
	}
	var users []User
	if err := tx.Find(&users).Error; err != nil {
		return nil, err
	}
	out := make([]BdPick, 0, len(users))
	for i := range users {
		out = append(out, bdUserPick(&users[i]))
	}
	return out, nil
}

// BdUserPicksByIds names the users already bound to customers.
func BdUserPicksByIds(ids []int) ([]BdPick, error) {
	if len(ids) == 0 {
		return []BdPick{}, nil
	}
	var users []User
	if err := DB.Select("id, username, display_name").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	out := make([]BdPick, 0, len(users))
	for i := range users {
		out = append(out, bdUserPick(&users[i]))
	}
	return out, nil
}
