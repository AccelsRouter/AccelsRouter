package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Business development (业务信息汇总): supplier and customer leads, each with
// the models they offer / need, and the records of which supplier model was
// allocated to which customer at what prices. Leads carry no prices; the
// supply price and the customer's acceptable price are agreed per
// allocation. Own tables only; no existing table is touched.

const (
	BdSideBuy  = "buy"  // 供应商：能给我们供货的一方，编号 B-001 起
	BdSideSell = "sell" // 客户：要买的一方，编号 S-001 起
)

// Lead stages, in pipeline order. "hold" (搁置) is excluded from matching
// and from new allocations.
var BdStages = []string{"contact", "confirm", "quote", "poc", "signing", "signed", "won", "hold"}

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
	Id        int    `json:"id" gorm:"primaryKey"`
	Side      string `json:"side" gorm:"size:8;not null;uniqueIndex:uk_bd_company_name,priority:1;uniqueIndex:uk_bd_company_seq,priority:1"`
	NameKey   string `json:"-" gorm:"size:128;not null;uniqueIndex:uk_bd_company_name,priority:2"`
	Name      string `json:"name" gorm:"size:128;not null"`
	Seq       int    `json:"seq" gorm:"not null;uniqueIndex:uk_bd_company_seq,priority:2"`
	Code      string `json:"code" gorm:"size:16;not null"`
	CreatedAt int64  `json:"created_at" gorm:"autoCreateTime"`
}

func (BdCompany) TableName() string { return "bd_companies" }

// BdLeadModel is one model a supplier offers (with its supply price) or a
// customer needs (with its expected price).
type BdLeadModel struct {
	Id         int    `json:"id" gorm:"primaryKey"`
	LeadId     int    `json:"lead_id" gorm:"index;not null"`
	Model      string `json:"model" gorm:"size:128;not null;index"`
	ModelClass string `json:"model_class" gorm:"size:64"`
	// % of the official list price, optional: the supplier's supply price or
	// the customer's expected price. The agreed prices are recorded per deal.
	PriceRate *float64 `json:"price_rate"`
	Sort      int      `json:"sort"`
}

func (BdLeadModel) TableName() string { return "bd_lead_models" }

type BdLead struct {
	Id         int           `json:"id" gorm:"primaryKey"`
	Side       string        `json:"side" gorm:"size:8;not null;index"`
	Code       string        `json:"code" gorm:"size:16;index"`
	Company    string        `json:"company" gorm:"size:128;not null;index"`
	Models     []BdLeadModel `json:"models" gorm:"-"`
	Currency   string        `json:"-" gorm:"column:currency;size:128"`
	Currencies []string      `json:"currency" gorm:"-"`
	Contact    string        `json:"contact" gorm:"size:128"`
	ContactWay string        `json:"contact_way" gorm:"size:255"`
	Region     string        `json:"region" gorm:"size:128"`
	Terms      string        `json:"terms" gorm:"size:64"`
	Stage      string        `json:"stage" gorm:"size:16;index"`
	OwnerId    int           `json:"owner_id" gorm:"index"`
	OwnerName  string        `json:"owner_name" gorm:"size:64"`
	Note       string        `json:"note" gorm:"type:text"`
	CreatedBy  int           `json:"created_by" gorm:"index"`
	UpdatedBy  int           `json:"updated_by"`
	CreatedAt  int64         `json:"created_at"`
	UpdatedAt  int64         `json:"updated_at"`
}

func (BdLead) TableName() string { return "bd_leads" }

func (l *BdLead) AfterFind(*gorm.DB) error {
	l.Currencies = splitBdList(l.Currency)
	return nil
}

// BdAllocation records that a supplier's model was allocated to a customer,
// with the supply price and the customer's acceptable price agreed for it
// (both % of the official list price). Company names are kept as a snapshot
// so a record stays readable after a lead is renamed.
type BdAllocation struct {
	Id             int      `json:"id" gorm:"primaryKey"`
	CustomerLeadId int      `json:"customer_lead_id" gorm:"not null;index;uniqueIndex:uk_bd_allocation,priority:1"`
	SupplierLeadId int      `json:"supplier_lead_id" gorm:"not null;index;uniqueIndex:uk_bd_allocation,priority:2"`
	Model          string   `json:"model" gorm:"size:128;not null;uniqueIndex:uk_bd_allocation,priority:3"`
	ModelClass     string   `json:"model_class" gorm:"size:64"`
	Customer       string   `json:"customer" gorm:"size:128"`
	Supplier       string   `json:"supplier" gorm:"size:128"`
	SupplyRate     *float64 `json:"supply_rate"` // 供货价 %
	SaleRate       *float64 `json:"sale_rate"`   // 可接受价 %
	AllocDate      string   `json:"alloc_date" gorm:"size:10;index"`
	OwnerId        int      `json:"owner_id" gorm:"index"`
	OwnerName      string   `json:"owner_name" gorm:"size:64"`
	Note           string   `json:"note" gorm:"type:text"`
	CreatedBy      int      `json:"created_by"`
	UpdatedBy      int      `json:"updated_by"`
	CreatedAt      int64    `json:"created_at"`
	UpdatedAt      int64    `json:"updated_at"`
}

func (BdAllocation) TableName() string { return "bd_allocations" }

// ---------- normalization ----------

var bdSpaces = regexp.MustCompile(`\s+`)

// cleanBdModel keeps a model name as typed, only trimming and collapsing
// whitespace.
func cleanBdModel(s string) string {
	return strings.TrimSpace(bdSpaces.ReplaceAllString(s, " "))
}

// BdModelKey compares model names across spellings: "claude-sonnet-4.6",
// "Claude Sonnet 4.6" and "claude_sonnet_4.6" all become "claudesonnet46".
func BdModelKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r > 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func bdNameKey(s string) string {
	return strings.ToLower(strings.TrimSpace(bdSpaces.ReplaceAllString(s, " ")))
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

// ListBdCompanies returns every coded supplier / customer, for suggesting
// names.
func ListBdCompanies() ([]BdCompany, error) {
	var rows []BdCompany
	err := DB.Order("side, seq").Find(&rows).Error
	return rows, err
}

// ---------- leads ----------

func bdModelIdentity(model, class string) string {
	return BdModelKey(model) + "\x00" + strings.ToLower(class)
}

func normalizeBdLead(l *BdLead) error {
	if l.Side != BdSideBuy && l.Side != BdSideSell {
		return bdInvalid("side must be buy or sell")
	}
	l.Company = bdClip(l.Company, 128)
	if l.Company == "" {
		return bdInvalid("company is required")
	}
	models := make([]BdLeadModel, 0, len(l.Models))
	seen := map[string]bool{}
	for _, m := range l.Models {
		m.Model = bdClip(cleanBdModel(m.Model), 128)
		m.ModelClass = bdClip(m.ModelClass, 64)
		if BdModelKey(m.Model) == "" {
			continue
		}
		if !bdValidRate(m.PriceRate) {
			return bdInvalid("price rate must be between 0 and 200")
		}
		key := bdModelIdentity(m.Model, m.ModelClass)
		if seen[key] {
			return bdInvalid("model %q is listed twice", m.Model)
		}
		seen[key] = true
		models = append(models, m)
	}
	if len(models) == 0 {
		return bdInvalid("at least one model is required")
	}
	if len(models) > 100 {
		return bdInvalid("too many models")
	}
	l.Models = models
	if l.Stage == "" {
		return bdInvalid("stage is required")
	}
	if !bdContains(BdStages, l.Stage) {
		return bdInvalid("unknown stage %q", l.Stage)
	}
	currencies := make([]string, 0, len(l.Currencies))
	for _, c := range l.Currencies {
		if bdContains(bdCurrencies, c) && !bdContains(currencies, c) {
			currencies = append(currencies, c)
		}
	}
	l.Currencies = currencies
	l.Currency = strings.Join(currencies, ",")
	l.Contact = bdClip(l.Contact, 128)
	l.ContactWay = bdClip(l.ContactWay, 255)
	l.Region = bdClip(l.Region, 128)
	l.Terms = bdClip(l.Terms, 64)
	l.Note = bdClip(l.Note, 4000)
	return nil
}

func attachBdModels(tx *gorm.DB, leads []BdLead) error {
	if len(leads) == 0 {
		return nil
	}
	ids := make([]int, len(leads))
	for i := range leads {
		ids[i] = leads[i].Id
	}
	var rows []BdLeadModel
	if err := tx.Where("lead_id IN ?", ids).Order("lead_id, sort, id").Find(&rows).Error; err != nil {
		return err
	}
	byLead := map[int][]BdLeadModel{}
	for _, r := range rows {
		byLead[r.LeadId] = append(byLead[r.LeadId], r)
	}
	for i := range leads {
		leads[i].Models = byLead[leads[i].Id]
		if leads[i].Models == nil {
			leads[i].Models = []BdLeadModel{}
		}
	}
	return nil
}

func ListBdLeads() ([]BdLead, error) {
	var rows []BdLead
	if err := DB.Order("updated_at DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, attachBdModels(DB, rows)
}

func getBdLeadTx(tx *gorm.DB, id int) (*BdLead, error) {
	var row BdLead
	err := tx.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBdNotFound
	}
	if err != nil {
		return nil, err
	}
	leads := []BdLead{row}
	if err := attachBdModels(tx, leads); err != nil {
		return nil, err
	}
	return &leads[0], nil
}

func GetBdLead(id int) (*BdLead, error) { return getBdLeadTx(DB, id) }

// SaveBdLead creates (Id == 0) or updates a lead and replaces its model list.
// A supplier model that is still referenced by an allocation cannot be
// removed. CreatedBy/CreatedAt of an existing lead are preserved.
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
		l.UpdatedAt = now
		l.UpdatedBy = operatorId
		if l.Id == 0 {
			l.CreatedAt = now
			l.CreatedBy = operatorId
			if err := tx.Create(l).Error; err != nil {
				return err
			}
		} else {
			old, err := getBdLeadTx(tx, l.Id)
			if err != nil {
				return err
			}
			if l.Side == BdSideBuy {
				kept := map[string]bool{}
				for _, m := range l.Models {
					kept[BdModelKey(m.Model)] = true
				}
				var allocated []BdAllocation
				if err := tx.Select("model").Where("supplier_lead_id = ?", l.Id).Find(&allocated).Error; err != nil {
					return err
				}
				for _, a := range allocated {
					if !kept[BdModelKey(a.Model)] {
						return bdInvalid("model %q is allocated to a customer; delete that allocation record first", a.Model)
					}
				}
			}
			l.Side = old.Side // a lead never changes side
			l.CreatedAt = old.CreatedAt
			l.CreatedBy = old.CreatedBy
			if err := tx.Model(&BdLead{Id: l.Id}).Select("*").Omit("id").Updates(l).Error; err != nil {
				return err
			}
			if err := tx.Where("lead_id = ?", l.Id).Delete(&BdLeadModel{}).Error; err != nil {
				return err
			}
		}
		for i := range l.Models {
			l.Models[i].Id = 0
			l.Models[i].LeadId = l.Id
			l.Models[i].Sort = i
		}
		if len(l.Models) > 0 {
			if err := tx.Create(&l.Models).Error; err != nil {
				return err
			}
		} else {
			l.Models = []BdLeadModel{}
		}
		// Keep allocation snapshots readable after a rename.
		col := "customer"
		idCol := "customer_lead_id"
		if l.Side == BdSideBuy {
			col, idCol = "supplier", "supplier_lead_id"
		}
		return tx.Model(&BdAllocation{}).Where(idCol+" = ?", l.Id).Update(col, l.Company).Error
	})
}

// DeleteBdLead deletes a lead that no allocation refers to.
func DeleteBdLead(id int) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&BdAllocation{}).Where("customer_lead_id = ? OR supplier_lead_id = ?", id, id).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return bdInvalid("this lead has %d allocation record(s); delete them first", n)
		}
		res := tx.Delete(&BdLead{}, id)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBdNotFound
		}
		return tx.Where("lead_id = ?", id).Delete(&BdLeadModel{}).Error
	})
}

// ---------- allocations ----------

func ListBdAllocations() ([]BdAllocation, error) {
	var rows []BdAllocation
	err := DB.Order("alloc_date DESC, id DESC").Find(&rows).Error
	return rows, err
}

func GetBdAllocation(id int) (*BdAllocation, error) {
	var row BdAllocation
	err := DB.First(&row, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBdNotFound
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// SaveBdAllocation creates (Id == 0) or updates an allocation. The model
// must be one the supplier offers; both prices are required; a customer gets
// a given supplier model only once.
func SaveBdAllocation(a *BdAllocation, operatorId int) error {
	a.Model = bdClip(cleanBdModel(a.Model), 128)
	a.Note = bdClip(a.Note, 4000)
	if BdModelKey(a.Model) == "" {
		return bdInvalid("model is required")
	}
	if a.SupplyRate == nil || a.SaleRate == nil {
		return bdInvalid("supply price and acceptable price are required")
	}
	if !bdValidRate(a.SupplyRate) || !bdValidRate(a.SaleRate) {
		return bdInvalid("price rate must be between 0 and 200")
	}
	if !validBdDate(a.AllocDate) {
		return bdInvalid("allocation date must be YYYY-MM-DD")
	}
	now := common.GetTimestamp()
	return DB.Transaction(func(tx *gorm.DB) error {
		customer, err := getBdLeadTx(tx, a.CustomerLeadId)
		if err != nil || customer.Side != BdSideSell {
			return bdInvalid("customer lead not found")
		}
		supplier, err := getBdLeadTx(tx, a.SupplierLeadId)
		if err != nil || supplier.Side != BdSideBuy {
			return bdInvalid("supplier lead not found")
		}
		var offered *BdLeadModel
		for i := range supplier.Models {
			if BdModelKey(supplier.Models[i].Model) == BdModelKey(a.Model) {
				offered = &supplier.Models[i]
				break
			}
		}
		if offered == nil {
			return bdInvalid("%s does not offer model %q", supplier.Company, a.Model)
		}
		a.Model = offered.Model // the supplier's spelling
		a.ModelClass = offered.ModelClass
		a.Customer = customer.Company
		a.Supplier = supplier.Company
		a.UpdatedAt = now
		a.UpdatedBy = operatorId

		var same []BdAllocation
		if err := tx.Select("id, model").Where("customer_lead_id = ? AND supplier_lead_id = ?", a.CustomerLeadId, a.SupplierLeadId).Find(&same).Error; err != nil {
			return err
		}
		for _, o := range same {
			if o.Id != a.Id && BdModelKey(o.Model) == BdModelKey(a.Model) {
				return bdInvalid("%s's %q is already allocated to %s", supplier.Company, a.Model, customer.Company)
			}
		}

		if a.Id == 0 {
			if customer.Stage == "hold" || supplier.Stage == "hold" {
				return bdInvalid("leads on hold cannot be allocated")
			}
			a.CreatedAt = now
			a.CreatedBy = operatorId
			return tx.Create(a).Error
		}
		var old BdAllocation
		if err := tx.First(&old, a.Id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrBdNotFound
			}
			return err
		}
		a.CreatedAt = old.CreatedAt
		a.CreatedBy = old.CreatedBy
		return tx.Model(&BdAllocation{Id: a.Id}).Select("*").Omit("id").Updates(a).Error
	})
}

func DeleteBdAllocation(id int) error {
	res := DB.Delete(&BdAllocation{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrBdNotFound
	}
	return nil
}

// ---------- owners and suggestions ----------

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

// ListBdModelNames returns the platform's model names for input suggestions.
func ListBdModelNames() []string {
	var names []string
	if DB.Model(&Model{}).Pluck("model_name", &names).Error != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if v := cleanBdModel(n); v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
