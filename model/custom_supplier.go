package model

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Supplier is an upstream supplier of model capacity (official APIs, cloud
// resellers, relays). It is unrelated to Vendor, which labels model makers.
// Due diligence fields live on the same row because they are strictly 1:1.
type Supplier struct {
	Id          int    `json:"id"`
	Code        string `json:"code" gorm:"type:varchar(40);uniqueIndex"`
	LegalName   string `json:"legal_name" gorm:"type:varchar(200)"`
	ShortName   string `json:"short_name" gorm:"type:varchar(64)"`
	RegPlace    string `json:"reg_place" gorm:"type:varchar(64)"`
	RegNo       string `json:"reg_no" gorm:"type:varchar(64)"`
	Website     string `json:"website" gorm:"type:varchar(255)"`
	Founded     string `json:"founded" gorm:"type:varchar(16)"`
	BizContact  string `json:"biz_contact" gorm:"type:varchar(255)"`
	TechContact string `json:"tech_contact" gorm:"type:varchar(255)"`
	Status      string `json:"status" gorm:"type:varchar(16);index"`
	Grade       string `json:"grade" gorm:"type:varchar(16)"`
	Owner       string `json:"owner" gorm:"type:varchar(64)"`
	OnboardDate string `json:"onboard_date" gorm:"type:varchar(10)"`
	SignEntity  string `json:"sign_entity" gorm:"type:varchar(16)"`
	// GstApplicable is nil until someone answers the question.
	GstApplicable *bool    `json:"gst_applicable"`
	GstRate       *float64 `json:"gst_rate" gorm:"type:decimal(5,2)"`

	KybStatus       string `json:"kyb_status" gorm:"type:varchar(16)"`
	PromptRetention string `json:"prompt_retention" gorm:"type:varchar(16)"`
	DataRegion      string `json:"data_region" gorm:"type:varchar(128)"`
	DpaSigned       *bool  `json:"dpa_signed"`
	Certifications  string `json:"certifications" gorm:"type:varchar(255)"`
	NextReviewDate  string `json:"next_review_date" gorm:"type:varchar(10)"`

	Remark string `json:"remark" gorm:"type:text"`

	CreatedBy   int            `json:"created_by"`
	UpdatedBy   int            `json:"updated_by"`
	CreatedTime int64          `json:"created_time" gorm:"type:bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"type:bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

// SupplierModel is one model channel a supplier offers. DiscountPct is the
// share of the list price paid, so 75 means 75% of list price.
type SupplierModel struct {
	Id          int      `json:"id"`
	SupplierId  int      `json:"supplier_id" gorm:"index"`
	ModelName   string   `json:"model_name" gorm:"type:varchar(128);index"`
	Variant     string   `json:"variant" gorm:"type:varchar(64)"`
	Source      string   `json:"source" gorm:"type:varchar(24)"`
	DiscountPct *float64 `json:"discount_pct" gorm:"type:decimal(6,2)"`
	Concurrency *int     `json:"concurrency"`
	Rpm         *int     `json:"rpm"`
	Tpm         *int64   `json:"tpm" gorm:"type:bigint"`
	Enabled     bool     `json:"enabled"`
	Remark      string   `json:"remark" gorm:"type:varchar(255)"`
	Sort        int      `json:"sort"`
}

const (
	SupplierStatusPotential  = "potential"
	SupplierStatusEvaluating = "evaluating"
	SupplierStatusTrial      = "trial"
	SupplierStatusActive     = "active"
	SupplierStatusWatch      = "watch"
	SupplierStatusSuspended  = "suspended"
	SupplierStatusTerminated = "terminated"
)

// supplierGstRatePct is the GST rate charged when a supplier charges GST.
const supplierGstRatePct = 9

var (
	supplierStatuses         = []string{SupplierStatusPotential, SupplierStatusEvaluating, SupplierStatusTrial, SupplierStatusActive, SupplierStatusWatch, SupplierStatusSuspended, SupplierStatusTerminated}
	supplierGrades           = []string{"strategic", "core", "backup", "temporary"}
	supplierSignEntities     = []string{"sg", "hk", "cn"}
	supplierKybStatuses      = []string{"complete", "partial", "none"}
	supplierPromptRetentions = []string{"none", "le_30d", "long_term", "unknown"}
	supplierModelSources     = []string{"official", "cloud_resale", "third_party_relay", "self_hosted", "account_pool"}
)

// supplierInvalid is a request error the web console can translate.
func supplierInvalid(key string, params ...map[string]any) error {
	return common.NewMessage(key, params...)
}

func checkSupplierEnum(field, value string, allowed []string) error {
	if value == "" || slices.Contains(allowed, value) {
		return nil
	}
	return supplierInvalid("Invalid value for {{field}}", map[string]any{"field": field})
}

func checkSupplierDate(field, value string) error {
	if value == "" {
		return nil
	}
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return supplierInvalid("Invalid value for {{field}}", map[string]any{"field": field})
	}
	return nil
}

func roundHundredths(value *float64) *float64 {
	if value == nil {
		return nil
	}
	rounded := math.Round(*value*100) / 100
	return &rounded
}

// normalize trims text, validates enums and dates, and clears values that the
// current answers make meaningless.
func (s *Supplier) normalize() error {
	for _, field := range []*string{&s.LegalName, &s.ShortName, &s.RegPlace, &s.RegNo, &s.Website, &s.Founded, &s.BizContact, &s.TechContact, &s.Owner, &s.DataRegion, &s.Certifications} {
		*field = strings.TrimSpace(*field)
	}
	checks := []error{
		checkSupplierEnum("status", s.Status, supplierStatuses),
		checkSupplierEnum("grade", s.Grade, supplierGrades),
		checkSupplierEnum("sign_entity", s.SignEntity, supplierSignEntities),
		checkSupplierEnum("kyb_status", s.KybStatus, supplierKybStatuses),
		checkSupplierEnum("prompt_retention", s.PromptRetention, supplierPromptRetentions),
		checkSupplierDate("onboard_date", s.OnboardDate),
		checkSupplierDate("next_review_date", s.NextReviewDate),
	}
	for _, err := range checks {
		if err != nil {
			return err
		}
	}
	if s.Status == "" {
		s.Status = SupplierStatusPotential
	}
	// GST is a yes/no answer at the Singapore rate; the rate is stored so a
	// later rate change does not rewrite what existing records said.
	s.GstRate = nil
	if s.GstApplicable != nil && *s.GstApplicable {
		rate := float64(supplierGstRatePct)
		s.GstRate = &rate
	}
	return nil
}

func normalizeSupplierModels(models []SupplierModel) ([]SupplierModel, error) {
	result := make([]SupplierModel, 0, len(models))
	for _, m := range models {
		m.ModelName = strings.TrimSpace(m.ModelName)
		m.Variant = strings.TrimSpace(m.Variant)
		m.Remark = strings.TrimSpace(m.Remark)
		blank := m.ModelName == "" && m.Variant == "" && m.Source == "" && m.DiscountPct == nil && m.Concurrency == nil && m.Rpm == nil && m.Tpm == nil && m.Remark == ""
		if blank {
			continue
		}
		if err := checkSupplierEnum("source", m.Source, supplierModelSources); err != nil {
			return nil, err
		}
		m.DiscountPct = roundHundredths(m.DiscountPct)
		if m.DiscountPct != nil && (*m.DiscountPct <= 0 || *m.DiscountPct > 1000) {
			return nil, supplierInvalid("Invalid value for {{field}}", map[string]any{"field": "discount_pct"})
		}
		if (m.Concurrency != nil && *m.Concurrency < 0) || (m.Rpm != nil && *m.Rpm < 0) || (m.Tpm != nil && *m.Tpm < 0) {
			return nil, supplierInvalid("Invalid value for {{field}}", map[string]any{"field": "limits"})
		}
		m.Id = 0
		m.Sort = len(result)
		result = append(result, m)
	}
	return result, nil
}

// SupplierMissingFields lists the required answers that are still empty. Keys
// are field names; model rows use "models.<row>.<field>" with 1-based rows and
// contract fields use "contract.<field>". Saving never fails on these: the
// list only marks the record as incomplete.
func SupplierMissingFields(s *Supplier, models []SupplierModel, current *SupplierContract) []string {
	missing := make([]string, 0)
	required := []struct {
		key   string
		empty bool
	}{
		{"legal_name", s.LegalName == ""},
		{"short_name", s.ShortName == ""},
		{"biz_contact", s.BizContact == ""},
		{"status", s.Status == ""},
		{"owner", s.Owner == ""},
		{"sign_entity", s.SignEntity == ""},
		{"gst_applicable", s.GstApplicable == nil},
	}
	for _, field := range required {
		if field.empty {
			missing = append(missing, field.key)
		}
	}
	if len(models) == 0 {
		missing = append(missing, "models")
	}
	for i, m := range models {
		row := strconv.Itoa(i + 1)
		if m.ModelName == "" {
			missing = append(missing, "models."+row+".model_name")
		}
		if m.Variant == "" {
			missing = append(missing, "models."+row+".variant")
		}
		if m.Source == "" {
			missing = append(missing, "models."+row+".source")
		}
		if m.DiscountPct == nil {
			missing = append(missing, "models."+row+".discount_pct")
		}
	}
	if current == nil {
		return append(missing, "contract")
	}
	return append(missing, current.MissingFields()...)
}

// SupplierListItem is a row of the supplier table.
type SupplierListItem struct {
	Supplier
	ModelCount   int               `json:"model_count"`
	ModelNames   []string          `json:"model_names"`
	Current      *SupplierContract `json:"current_contract"`
	MissingCount int               `json:"missing_count"`
	LowBalance   bool              `json:"low_balance"`
}

type SupplierDetail struct {
	Supplier
	Models        []SupplierModel      `json:"models"`
	Contracts     []SupplierContract   `json:"contracts"`
	Attachments   []SupplierAttachment `json:"attachments"`
	MissingFields []string             `json:"missing_fields"`
}

type SupplierStats struct {
	Total      int `json:"total"`
	Active     int `json:"active"`
	ModelCount int `json:"model_count"`
	LowBalance int `json:"low_balance"`
	Incomplete int `json:"incomplete"`
}

func supplierKeywordQuery(query *gorm.DB, keyword string) *gorm.DB {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return query
	}
	like := "%" + keyword + "%"
	modelSuppliers := DB.Model(&SupplierModel{}).Select("supplier_id").Where("LOWER(model_name) LIKE ?", like)
	return query.Where("LOWER(legal_name) LIKE ? OR LOWER(short_name) LIKE ? OR LOWER(code) LIKE ? OR id IN (?)", like, like, like, modelSuppliers)
}

func loadSupplierModels(ids []int) (map[int][]SupplierModel, error) {
	result := make(map[int][]SupplierModel, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var models []SupplierModel
	if err := DB.Where("supplier_id IN ?", ids).Order("supplier_id asc, sort asc, id asc").Find(&models).Error; err != nil {
		return nil, err
	}
	for _, m := range models {
		result[m.SupplierId] = append(result[m.SupplierId], m)
	}
	return result, nil
}

func loadCurrentContracts(ids []int) (map[int]*SupplierContract, error) {
	result := make(map[int]*SupplierContract, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	var contracts []SupplierContract
	if err := DB.Where("supplier_id IN ? AND is_current = ?", ids, true).Find(&contracts).Error; err != nil {
		return nil, err
	}
	for i := range contracts {
		result[contracts[i].SupplierId] = &contracts[i]
	}
	return result, nil
}

func buildSupplierListItems(suppliers []Supplier) ([]SupplierListItem, error) {
	ids := make([]int, 0, len(suppliers))
	for _, s := range suppliers {
		ids = append(ids, s.Id)
	}
	models, err := loadSupplierModels(ids)
	if err != nil {
		return nil, err
	}
	currents, err := loadCurrentContracts(ids)
	if err != nil {
		return nil, err
	}
	items := make([]SupplierListItem, 0, len(suppliers))
	for i := range suppliers {
		s := suppliers[i]
		names := make([]string, 0, len(models[s.Id]))
		for _, m := range models[s.Id] {
			if m.ModelName != "" {
				names = append(names, m.ModelName)
			}
		}
		current := currents[s.Id]
		items = append(items, SupplierListItem{
			Supplier:     s,
			ModelCount:   len(models[s.Id]),
			ModelNames:   names,
			Current:      current,
			MissingCount: len(SupplierMissingFields(&s, models[s.Id], current)),
			LowBalance:   current != nil && current.LowBalance(),
		})
	}
	return items, nil
}

func SearchSuppliers(keyword, status string, startIdx, pageSize int) ([]SupplierListItem, int64, error) {
	query := supplierKeywordQuery(DB.Model(&Supplier{}), keyword)
	if status != "" {
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var suppliers []Supplier
	if err := query.Order("id desc").Offset(startIdx).Limit(pageSize).Find(&suppliers).Error; err != nil {
		return nil, 0, err
	}
	items, err := buildSupplierListItems(suppliers)
	return items, total, err
}

// GetSupplierStats scans every supplier; the table is a hand-maintained
// register expected to stay in the hundreds of rows.
func GetSupplierStats() (SupplierStats, error) {
	var suppliers []Supplier
	if err := DB.Find(&suppliers).Error; err != nil {
		return SupplierStats{}, err
	}
	items, err := buildSupplierListItems(suppliers)
	if err != nil {
		return SupplierStats{}, err
	}
	stats := SupplierStats{Total: len(items)}
	modelNames := map[string]struct{}{}
	for _, item := range items {
		if item.Status == SupplierStatusActive {
			stats.Active++
		}
		if item.LowBalance {
			stats.LowBalance++
		}
		if item.MissingCount > 0 {
			stats.Incomplete++
		}
		for _, name := range item.ModelNames {
			modelNames[name] = struct{}{}
		}
	}
	stats.ModelCount = len(modelNames)
	return stats, nil
}

func GetSupplierDetail(id int) (*SupplierDetail, error) {
	var s Supplier
	if err := DB.First(&s, id).Error; err != nil {
		return nil, err
	}
	detail := &SupplierDetail{Supplier: s}
	if err := DB.Where("supplier_id = ?", id).Order("sort asc, id asc").Find(&detail.Models).Error; err != nil {
		return nil, err
	}
	if err := DB.Where("supplier_id = ?", id).Order("id desc").Find(&detail.Contracts).Error; err != nil {
		return nil, err
	}
	contractIds := make([]int, 0, len(detail.Contracts))
	var current *SupplierContract
	for i := range detail.Contracts {
		contractIds = append(contractIds, detail.Contracts[i].Id)
		if detail.Contracts[i].IsCurrent {
			current = &detail.Contracts[i]
		}
	}
	attachments, err := listSupplierAttachments(id, contractIds)
	if err != nil {
		return nil, err
	}
	detail.Attachments = attachments
	detail.MissingFields = SupplierMissingFields(&detail.Supplier, detail.Models, current)
	return detail, nil
}

// supplierEditableFields copies the fields a client may set.
func supplierEditableFields(dst *Supplier, src *Supplier) {
	dst.LegalName, dst.ShortName, dst.RegPlace, dst.RegNo = src.LegalName, src.ShortName, src.RegPlace, src.RegNo
	dst.Website, dst.Founded, dst.BizContact, dst.TechContact = src.Website, src.Founded, src.BizContact, src.TechContact
	dst.Status, dst.Grade, dst.Owner, dst.OnboardDate = src.Status, src.Grade, src.Owner, src.OnboardDate
	dst.SignEntity, dst.GstApplicable, dst.GstRate = src.SignEntity, src.GstApplicable, src.GstRate
	dst.KybStatus, dst.PromptRetention, dst.DataRegion, dst.DpaSigned = src.KybStatus, src.PromptRetention, src.DataRegion, src.DpaSigned
	dst.Certifications, dst.NextReviewDate, dst.Remark = src.Certifications, src.NextReviewDate, src.Remark
}

func replaceSupplierModels(tx *gorm.DB, supplierID int, models []SupplierModel) error {
	if err := tx.Where("supplier_id = ?", supplierID).Delete(&SupplierModel{}).Error; err != nil {
		return err
	}
	if len(models) == 0 {
		return nil
	}
	for i := range models {
		models[i].SupplierId = supplierID
	}
	return tx.Create(&models).Error
}

// CreateSupplier saves a supplier and its models in one transaction. The code
// is derived from the generated id so it never repeats.
func CreateSupplier(input *Supplier, models []SupplierModel, operatorID int) (*Supplier, error) {
	s := &Supplier{}
	supplierEditableFields(s, input)
	if err := s.normalize(); err != nil {
		return nil, err
	}
	normalizedModels, err := normalizeSupplierModels(models)
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	s.CreatedBy, s.UpdatedBy, s.CreatedTime, s.UpdatedTime = operatorID, operatorID, now, now
	// A unique placeholder until the id is known.
	s.Code = "p-" + common.GetUUID()
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(s).Error; err != nil {
			return err
		}
		s.Code = fmt.Sprintf("V%03d", s.Id)
		if err := tx.Model(&Supplier{}).Where("id = ?", s.Id).Update("code", s.Code).Error; err != nil {
			return err
		}
		return replaceSupplierModels(tx, s.Id, normalizedModels)
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// UpdateSupplier replaces the editable fields and the whole model list and
// returns the record as it was before the change.
func UpdateSupplier(id int, input *Supplier, models []SupplierModel, operatorID int) (before *Supplier, after *Supplier, err error) {
	normalizedModels, err := normalizeSupplierModels(models)
	if err != nil {
		return nil, nil, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		var current Supplier
		if err := lockForUpdate(tx).First(&current, id).Error; err != nil {
			return err
		}
		previous := current
		supplierEditableFields(&current, input)
		if err := current.normalize(); err != nil {
			return err
		}
		current.UpdatedBy, current.UpdatedTime = operatorID, common.GetTimestamp()
		if err := tx.Model(&Supplier{Id: id}).Select("*").Omit("id", "code", "created_by", "created_time", "deleted_at").Updates(&current).Error; err != nil {
			return err
		}
		if err := replaceSupplierModels(tx, id, normalizedModels); err != nil {
			return err
		}
		before, after = &previous, &current
		return nil
	})
	return before, after, err
}

// DeleteSupplier soft-deletes the supplier. Contract numbers stay taken.
func DeleteSupplier(id int) (*Supplier, error) {
	var s Supplier
	if err := DB.First(&s, id).Error; err != nil {
		return nil, err
	}
	if err := DB.Delete(&Supplier{}, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// SupplierAuditChanges compares two versions of a supplier record (supplier
// or contract) for the audit log by their JSON fields. Contact details are
// reported as changed without their values.
func SupplierAuditChanges(before, after any) map[string]any {
	changes := map[string]any{}
	var oldValues, newValues map[string]any
	oldJSON, err := common.Marshal(before)
	if err != nil {
		return changes
	}
	newJSON, err := common.Marshal(after)
	if err != nil {
		return changes
	}
	if common.Unmarshal(oldJSON, &oldValues) != nil || common.Unmarshal(newJSON, &newValues) != nil {
		return changes
	}
	for key, newValue := range newValues {
		if key == "updated_by" || key == "updated_time" {
			continue
		}
		oldValue := oldValues[key]
		if fmt.Sprint(oldValue) == fmt.Sprint(newValue) {
			continue
		}
		if key == "biz_contact" || key == "tech_contact" {
			changes[key] = "changed"
			continue
		}
		changes[key] = map[string]any{"from": oldValue, "to": newValue}
	}
	return changes
}
