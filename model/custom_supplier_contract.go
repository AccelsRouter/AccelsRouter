package model

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SupplierMoney is an amount in hundredths of the contract currency. It is
// stored as an integer and exchanged as a decimal string such as "1200.50",
// so amounts never pass through floating point.
type SupplierMoney int64

func (m SupplierMoney) String() string {
	value := int64(m)
	sign := ""
	if value < 0 {
		sign, value = "-", -value
	}
	return fmt.Sprintf("%s%d.%02d", sign, value/100, value%100)
}

func (m SupplierMoney) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

func (m *SupplierMoney) UnmarshalJSON(data []byte) error {
	text := strings.Trim(strings.TrimSpace(string(data)), `"`)
	parsed, err := ParseSupplierMoney(text)
	if err != nil {
		return err
	}
	*m = parsed
	return nil
}

// ParseSupplierMoney accepts a non-negative decimal with at most two places.
func ParseSupplierMoney(text string) (SupplierMoney, error) {
	invalid := common.NewMessage("Invalid amount {{amount}}", map[string]any{"amount": text})
	whole, fraction, hasFraction := strings.Cut(strings.TrimSpace(text), ".")
	if whole == "" || len(whole) > 15 || len(fraction) > 2 || (hasFraction && fraction == "") {
		return 0, invalid
	}
	for _, r := range whole + fraction {
		if r < '0' || r > '9' {
			return 0, invalid
		}
	}
	units, err := strconv.ParseInt(whole, 10, 64)
	if err != nil {
		return 0, invalid
	}
	cents := int64(0)
	if fraction != "" {
		cents, _ = strconv.ParseInt(fraction+strings.Repeat("0", 2-len(fraction)), 10, 64)
	}
	return SupplierMoney(units*100 + cents), nil
}

const (
	SupplierContractFramework = "framework"
	SupplierContractSingle    = "single"
	SupplierContractOrder     = "order"

	SupplierContractActive = "active"
	SupplierContractVoid   = "void"

	// Supplier contracts are purchases, direction P in the contract numbering scheme.
	supplierContractDirection = "P"
	supplierOrderLimit        = 99
	defaultLowBalancePct      = 20
)

var (
	supplierBusinessLines = []string{"GW", "MA", "TL", "CO"}
	supplierContractTypes = map[string]string{SupplierContractFramework: "FA", SupplierContractSingle: "SA", SupplierContractOrder: ""}
	supplierCurrencies    = []string{"USD", "CNY", "USDT", "USDC"}
	supplierPayModes      = []string{"prepaid", "postpaid"}

	// The contract year follows company time (UTC+8), not server time, so a
	// number taken on the evening of 31 December UTC belongs to the new year.
	supplierContractZone = time.FixedZone("UTC+8", 8*60*60)
)

// SupplierContract is one contract with a supplier. Business line, type and
// parent framework are fixed once the number is taken; a wrong number is
// voided and a new contract is created, and voided numbers are never reused.
type SupplierContract struct {
	Id               int            `json:"id"`
	SupplierId       int            `json:"supplier_id" gorm:"index"`
	BusinessLine     string         `json:"business_line" gorm:"type:varchar(2)"`
	ContractType     string         `json:"contract_type" gorm:"type:varchar(16)"`
	ParentContractId int            `json:"parent_contract_id" gorm:"index"`
	ContractNo       string         `json:"contract_no" gorm:"type:varchar(40);uniqueIndex"`
	Status           string         `json:"status" gorm:"type:varchar(16)"`
	IsCurrent        bool           `json:"is_current"`
	OrderSeq         int            `json:"-"`
	StartDate        string         `json:"start_date" gorm:"type:varchar(10)"`
	EndDate          string         `json:"end_date" gorm:"type:varchar(10)"`
	AutoRenew        *bool          `json:"auto_renew"`
	NoticePeriod     string         `json:"notice_period" gorm:"type:varchar(32)"`
	Currency         string         `json:"currency" gorm:"type:varchar(8)"`
	PayMode          string         `json:"pay_mode" gorm:"type:varchar(16)"`
	PaymentTerm      string         `json:"payment_term" gorm:"type:varchar(64)"`
	RebateRule       string         `json:"rebate_rule" gorm:"type:text"`
	PrepaidAmount    *SupplierMoney `json:"prepaid_amount" gorm:"type:bigint"`
	RemainingAmount  *SupplierMoney `json:"remaining_amount" gorm:"type:bigint"`
	LowBalancePct    float64        `json:"low_balance_pct" gorm:"type:decimal(5,2)"`
	SlaTerms         string         `json:"sla_terms" gorm:"type:text"`
	Remark           string         `json:"remark" gorm:"type:text"`
	VoidReason       string         `json:"void_reason" gorm:"type:varchar(255)"`
	VoidedTime       int64          `json:"voided_time" gorm:"type:bigint"`
	CreatedBy        int            `json:"created_by"`
	UpdatedBy        int            `json:"updated_by"`
	CreatedTime      int64          `json:"created_time" gorm:"type:bigint"`
	UpdatedTime      int64          `json:"updated_time" gorm:"type:bigint"`
	// ModelIds are the supplier model rows the contract covers.
	ModelIds []int `json:"model_ids" gorm:"-"`
}

// SupplierContractModel links a contract to a supplier model row it covers.
type SupplierContractModel struct {
	Id              int `json:"id"`
	ContractId      int `json:"contract_id" gorm:"uniqueIndex:uk_supplier_contract_model,priority:1"`
	SupplierModelId int `json:"supplier_model_id" gorm:"uniqueIndex:uk_supplier_contract_model,priority:2;index"`
}

// setSupplierContractModels replaces the models a contract covers. Every id
// must be a model row of the contract's supplier.
func setSupplierContractModels(tx *gorm.DB, c *SupplierContract, modelIDs []int) error {
	ids := slices.Compact(slices.Sorted(slices.Values(modelIDs)))
	if len(ids) > 0 {
		var owned int64
		if err := tx.Model(&SupplierModel{}).Where("supplier_id = ? AND id IN ?", c.SupplierId, ids).Count(&owned).Error; err != nil {
			return err
		}
		if int(owned) != len(ids) {
			return supplierInvalid("Pick models from this supplier's model list")
		}
	}
	if err := tx.Where("contract_id = ?", c.Id).Delete(&SupplierContractModel{}).Error; err != nil {
		return err
	}
	links := make([]SupplierContractModel, 0, len(ids))
	for _, id := range ids {
		links = append(links, SupplierContractModel{ContractId: c.Id, SupplierModelId: id})
	}
	if len(links) > 0 {
		if err := tx.Create(&links).Error; err != nil {
			return err
		}
	}
	c.ModelIds = ids
	return nil
}

// fillSupplierContractModels loads the covered model ids of contracts.
func fillSupplierContractModels(db *gorm.DB, contracts []SupplierContract) error {
	if len(contracts) == 0 {
		return nil
	}
	ids := make([]int, 0, len(contracts))
	for i := range contracts {
		ids = append(ids, contracts[i].Id)
		contracts[i].ModelIds = []int{}
	}
	var links []SupplierContractModel
	if err := db.Where("contract_id IN ?", ids).Order("supplier_model_id").Find(&links).Error; err != nil {
		return err
	}
	byContract := map[int][]int{}
	for _, link := range links {
		byContract[link.ContractId] = append(byContract[link.ContractId], link.SupplierModelId)
	}
	for i := range contracts {
		if covered, ok := byContract[contracts[i].Id]; ok {
			contracts[i].ModelIds = covered
		}
	}
	return nil
}

// SupplierContractSeq counts main contract numbers per direction and year.
type SupplierContractSeq struct {
	Direction string `gorm:"primaryKey;type:varchar(1)"`
	Year      int    `gorm:"primaryKey;autoIncrement:false"`
	LastNo    int
}

func (c *SupplierContract) isPrepaid() bool { return c.PayMode == "prepaid" }

// MissingFields lists the empty required contract answers as "contract.<field>".
func (c *SupplierContract) MissingFields() []string {
	required := []struct {
		key   string
		empty bool
	}{
		{"end_date", c.EndDate == ""},
		{"auto_renew", c.AutoRenew == nil},
		{"notice_period", c.NoticePeriod == ""},
		{"currency", c.Currency == ""},
		{"pay_mode", c.PayMode == ""},
		{"prepaid_amount", c.isPrepaid() && c.PrepaidAmount == nil},
	}
	missing := make([]string, 0)
	for _, field := range required {
		if field.empty {
			missing = append(missing, "contract."+field.key)
		}
	}
	return missing
}

// LowBalance reports a prepaid balance under the contract's warning threshold.
func (c *SupplierContract) LowBalance() bool {
	if !c.isPrepaid() || c.PrepaidAmount == nil || c.RemainingAmount == nil || *c.PrepaidAmount <= 0 {
		return false
	}
	return float64(*c.RemainingAmount)*100 < c.LowBalancePct*float64(*c.PrepaidAmount)
}

func (c *SupplierContract) normalizeTerms() error {
	for _, field := range []*string{&c.NoticePeriod, &c.PaymentTerm, &c.VoidReason} {
		*field = strings.TrimSpace(*field)
	}
	checks := []error{
		checkSupplierEnum("currency", c.Currency, supplierCurrencies),
		checkSupplierEnum("pay_mode", c.PayMode, supplierPayModes),
		checkSupplierDate("start_date", c.StartDate),
		checkSupplierDate("end_date", c.EndDate),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if c.StartDate != "" && c.EndDate != "" && c.EndDate < c.StartDate {
		return supplierInvalid("The end date must not be earlier than the start date")
	}
	if c.LowBalancePct <= 0 {
		c.LowBalancePct = defaultLowBalancePct
	}
	if c.LowBalancePct > 100 {
		return supplierInvalid("Invalid value for {{field}}", map[string]any{"field": "low_balance_pct"})
	}
	pct := roundHundredths(&c.LowBalancePct)
	c.LowBalancePct = *pct
	return nil
}

// contractTermFields copies the terms a client may change after numbering.
func contractTermFields(dst *SupplierContract, src *SupplierContract) {
	dst.StartDate, dst.EndDate, dst.AutoRenew, dst.NoticePeriod = src.StartDate, src.EndDate, src.AutoRenew, src.NoticePeriod
	dst.Currency, dst.PayMode, dst.PaymentTerm, dst.RebateRule = src.Currency, src.PayMode, src.PaymentTerm, src.RebateRule
	dst.PrepaidAmount, dst.RemainingAmount, dst.LowBalancePct = src.PrepaidAmount, src.RemainingAmount, src.LowBalancePct
	dst.SlaTerms, dst.Remark = src.SlaTerms, src.Remark
}

// touchSupplier serializes contract changes of one supplier: the UPDATE takes
// the row's write lock on every supported database until the transaction ends.
// Writing first also keeps SQLite from upgrading a read lock mid-transaction.
// Existence is checked afterwards because MySQL reports zero affected rows
// when an UPDATE leaves the values unchanged.
func touchSupplier(tx *gorm.DB, supplierID, operatorID int) error {
	if err := tx.Model(&Supplier{}).Where("id = ?", supplierID).Updates(map[string]any{"updated_by": operatorID, "updated_time": common.GetTimestamp()}).Error; err != nil {
		return err
	}
	return tx.Select("id").First(&Supplier{}, supplierID).Error
}

func loadFrameworkParent(tx *gorm.DB, supplierID, parentID int) (*SupplierContract, error) {
	var parent SupplierContract
	err := tx.Where("id = ? AND supplier_id = ?", parentID, supplierID).First(&parent).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, supplierInvalid("Select a framework agreement of this supplier")
	}
	if err != nil {
		return nil, err
	}
	if parent.ContractType != SupplierContractFramework || parent.Status != SupplierContractActive {
		return nil, supplierInvalid("Select a framework agreement of this supplier")
	}
	return &parent, nil
}

func contractYear(now time.Time) int { return now.In(supplierContractZone).Year() % 100 }

func formatMainContractNo(businessLine, contractType string, year, seq int) string {
	return fmt.Sprintf("ACL-%s-%s-%s-%02d-%04d", supplierContractDirection, businessLine, supplierContractTypes[contractType], year, seq)
}

// takeSupplierContractNo assigns the contract number inside tx. Counters are
// raised with an atomic UPDATE before they are read, so concurrent takers wait
// on the row lock instead of reading the same value.
func takeSupplierContractNo(tx *gorm.DB, c *SupplierContract, now time.Time) error {
	if c.ContractType == SupplierContractOrder {
		parent, err := loadFrameworkParent(tx, c.SupplierId, c.ParentContractId)
		if err != nil {
			return err
		}
		if err := tx.Model(&SupplierContract{}).Where("id = ?", parent.Id).Update("order_seq", gorm.Expr("order_seq + ?", 1)).Error; err != nil {
			return err
		}
		var seq int
		if err := tx.Model(&SupplierContract{}).Where("id = ?", parent.Id).Select("order_seq").Scan(&seq).Error; err != nil {
			return err
		}
		if seq > supplierOrderLimit {
			return supplierInvalid("This framework agreement already has {{count}} orders", map[string]any{"count": supplierOrderLimit})
		}
		c.BusinessLine = parent.BusinessLine
		c.ContractNo = fmt.Sprintf("%s-O%02d", parent.ContractNo, seq)
		return nil
	}

	year := contractYear(now)
	counter := SupplierContractSeq{Direction: supplierContractDirection, Year: year}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&counter).Error; err != nil {
		return err
	}
	where := tx.Model(&SupplierContractSeq{}).Where("direction = ? AND year = ?", supplierContractDirection, year)
	if err := where.Update("last_no", gorm.Expr("last_no + ?", 1)).Error; err != nil {
		return err
	}
	var seq int
	if err := tx.Model(&SupplierContractSeq{}).Where("direction = ? AND year = ?", supplierContractDirection, year).Select("last_no").Scan(&seq).Error; err != nil {
		return err
	}
	c.ContractNo = formatMainContractNo(c.BusinessLine, c.ContractType, year, seq)
	return nil
}

func validateContractIdentity(c *SupplierContract) error {
	if _, ok := supplierContractTypes[c.ContractType]; !ok {
		return supplierInvalid("Select a contract type")
	}
	if c.ContractType == SupplierContractOrder {
		if c.ParentContractId <= 0 {
			return supplierInvalid("Select a framework agreement of this supplier")
		}
		return nil
	}
	c.ParentContractId = 0
	if err := checkSupplierEnum("business_line", c.BusinessLine, supplierBusinessLines); err != nil || c.BusinessLine == "" {
		return supplierInvalid("Select a business line")
	}
	return nil
}

// PreviewSupplierContractNo shows the next number without taking it.
func PreviewSupplierContractNo(supplierID int, businessLine, contractType string, parentID int) (string, error) {
	c := &SupplierContract{SupplierId: supplierID, BusinessLine: businessLine, ContractType: contractType, ParentContractId: parentID}
	if err := validateContractIdentity(c); err != nil {
		return "", err
	}
	if c.ContractType == SupplierContractOrder {
		parent, err := loadFrameworkParent(DB, supplierID, parentID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s-O%02d", parent.ContractNo, parent.OrderSeq+1), nil
	}
	year := contractYear(time.Now())
	var counter SupplierContractSeq
	err := DB.Where("direction = ? AND year = ?", supplierContractDirection, year).First(&counter).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	return formatMainContractNo(c.BusinessLine, c.ContractType, year, counter.LastNo+1), nil
}

// CreateSupplierContract takes a number and stores the contract. The first
// active contract of a supplier becomes its current contract.
func CreateSupplierContract(supplierID int, input *SupplierContract, operatorID int) (*SupplierContract, error) {
	c := &SupplierContract{
		SupplierId:       supplierID,
		BusinessLine:     input.BusinessLine,
		ContractType:     input.ContractType,
		ParentContractId: input.ParentContractId,
		Status:           SupplierContractActive,
	}
	contractTermFields(c, input)
	if err := validateContractIdentity(c); err != nil {
		return nil, err
	}
	if err := c.normalizeTerms(); err != nil {
		return nil, err
	}
	now := time.Now()
	c.CreatedBy, c.UpdatedBy, c.CreatedTime, c.UpdatedTime = operatorID, operatorID, now.Unix(), now.Unix()
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := touchSupplier(tx, supplierID, operatorID); err != nil {
			return err
		}
		if err := takeSupplierContractNo(tx, c, now); err != nil {
			return err
		}
		var currentCount int64
		if err := tx.Model(&SupplierContract{}).Where("supplier_id = ? AND is_current = ?", supplierID, true).Count(&currentCount).Error; err != nil {
			return err
		}
		c.IsCurrent = currentCount == 0
		if err := tx.Create(c).Error; err != nil {
			return err
		}
		return setSupplierContractModels(tx, c, input.ModelIds)
	})
	if err != nil {
		return nil, err
	}
	return c, nil
}

// lockedSupplierContract locks the contract's supplier, then reads the
// contract. The lock is taken through a subquery so the transaction writes
// before it reads.
func lockedSupplierContract(tx *gorm.DB, id, operatorID int) (*SupplierContract, error) {
	owner := tx.Model(&SupplierContract{}).Select("supplier_id").Where("id = ?", id)
	if err := tx.Model(&Supplier{}).Where("id = (?)", owner).Updates(map[string]any{"updated_by": operatorID, "updated_time": common.GetTimestamp()}).Error; err != nil {
		return nil, err
	}
	var c SupplierContract
	if err := tx.First(&c, id).Error; err != nil {
		return nil, err
	}
	if err := tx.Select("id").First(&Supplier{}, c.SupplierId).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// UpdateSupplierContract changes the terms of an active contract.
func UpdateSupplierContract(id int, input *SupplierContract, operatorID int) (before *SupplierContract, after *SupplierContract, err error) {
	err = DB.Transaction(func(tx *gorm.DB) error {
		c, err := lockedSupplierContract(tx, id, operatorID)
		if err != nil {
			return err
		}
		if c.Status != SupplierContractActive {
			return supplierInvalid("A voided contract cannot be changed")
		}
		previous := *c
		contractTermFields(c, input)
		if err := c.normalizeTerms(); err != nil {
			return err
		}
		c.UpdatedBy, c.UpdatedTime = operatorID, common.GetTimestamp()
		terms := []string{"start_date", "end_date", "auto_renew", "notice_period", "currency", "pay_mode", "payment_term", "rebate_rule", "prepaid_amount", "remaining_amount", "low_balance_pct", "sla_terms", "remark", "updated_by", "updated_time"}
		if err := tx.Model(&SupplierContract{Id: id}).Select(terms).Updates(c).Error; err != nil {
			return err
		}
		covered := []SupplierContract{previous}
		if err := fillSupplierContractModels(tx, covered); err != nil {
			return err
		}
		previous.ModelIds, c.ModelIds = covered[0].ModelIds, covered[0].ModelIds
		// A request without model_ids leaves the covered models as they are.
		if input.ModelIds != nil {
			if err := setSupplierContractModels(tx, c, input.ModelIds); err != nil {
				return err
			}
		}
		before, after = &previous, c
		return nil
	})
	return before, after, err
}

// VoidSupplierContract retires a contract number for good.
func VoidSupplierContract(id int, reason string, operatorID int) (*SupplierContract, error) {
	var voided *SupplierContract
	err := DB.Transaction(func(tx *gorm.DB) error {
		c, err := lockedSupplierContract(tx, id, operatorID)
		if err != nil {
			return err
		}
		if c.Status == SupplierContractVoid {
			return supplierInvalid("This contract is already void")
		}
		if c.ContractType == SupplierContractFramework {
			var activeOrders int64
			if err := tx.Model(&SupplierContract{}).Where("parent_contract_id = ? AND status = ?", c.Id, SupplierContractActive).Count(&activeOrders).Error; err != nil {
				return err
			}
			if activeOrders > 0 {
				return supplierInvalid("Void the orders under this framework agreement first")
			}
		}
		now := common.GetTimestamp()
		c.Status, c.IsCurrent, c.VoidReason, c.VoidedTime = SupplierContractVoid, false, strings.TrimSpace(reason), now
		c.UpdatedBy, c.UpdatedTime = operatorID, now
		if err := tx.Model(&SupplierContract{Id: id}).Select("status", "is_current", "void_reason", "voided_time", "updated_by", "updated_time").Updates(c).Error; err != nil {
			return err
		}
		voided = c
		return nil
	})
	return voided, err
}

// SetCurrentSupplierContract makes an active contract the one shown in lists.
func SetCurrentSupplierContract(id int, operatorID int) (*SupplierContract, error) {
	var current *SupplierContract
	err := DB.Transaction(func(tx *gorm.DB) error {
		c, err := lockedSupplierContract(tx, id, operatorID)
		if err != nil {
			return err
		}
		if c.Status != SupplierContractActive {
			return supplierInvalid("A voided contract cannot be changed")
		}
		if err := tx.Model(&SupplierContract{}).Where("supplier_id = ? AND id <> ?", c.SupplierId, c.Id).Update("is_current", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&SupplierContract{Id: id}).Update("is_current", true).Error; err != nil {
			return err
		}
		c.IsCurrent = true
		current = c
		return nil
	})
	return current, err
}

// ListSupplierFrameworks returns the active framework agreements an order can
// belong to.
func ListSupplierFrameworks(supplierID int) ([]SupplierContract, error) {
	var contracts []SupplierContract
	err := DB.Where("supplier_id = ? AND contract_type = ? AND status = ?", supplierID, SupplierContractFramework, SupplierContractActive).Order("id desc").Find(&contracts).Error
	return contracts, err
}

func GetSupplierContract(id int) (*SupplierContract, error) {
	var c SupplierContract
	if err := DB.First(&c, id).Error; err != nil {
		return nil, err
	}
	return &c, nil
}
