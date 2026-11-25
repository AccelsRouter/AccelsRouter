package model

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Supplier records and BD purchase leads name the same companies and share
// one number: a supplier record's code is the code of its BD supplier
// company (B-001…). Creating either one creates the other with the same name
// when it does not exist yet, inside the same transaction. Only creation is
// mirrored; later edits stay on their own side.

// supplierBdModelNames lists the models a BD lead may carry. A variable so
// the database matrix test, whose tables are prefixed, can supply its own.
var supplierBdModelNames = ListBdModelNames

// supplierDisplayName is the name a supplier is known by in the BD summary.
func supplierDisplayName(s *Supplier) string {
	if name := strings.TrimSpace(s.ShortName); name != "" {
		return name
	}
	return strings.TrimSpace(s.LegalName)
}

// supplierBdCompany returns the BD supplier company of a supplier record:
// the one named like its short name, else like its legal name, else a new
// one under its display name (created is then true). A record without any
// name has none.
func supplierBdCompany(tx *gorm.DB, s *Supplier) (company *BdCompany, created bool, err error) {
	for _, name := range []string{s.ShortName, s.LegalName} {
		key := bdNameKey(name)
		if key == "" {
			continue
		}
		var found BdCompany
		err := tx.Where("side = ? AND name_key = ?", BdSideBuy, key).First(&found).Error
		if err == nil {
			return &found, false, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}
	}
	name := supplierDisplayName(s)
	if name == "" {
		return nil, false, nil
	}
	company, err = EnsureBdCompany(tx, BdSideBuy, name)
	return company, err == nil, err
}

// checkSupplierCodeFree fails when another live supplier record (not
// exceptID) already holds code, that is, already stands for the company.
func checkSupplierCodeFree(tx *gorm.DB, code string, exceptID int) error {
	var taken Supplier
	err := tx.Select("id", "code").Where("code = ? AND id <> ?", code, exceptID).First(&taken).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return supplierInvalid("Supplier record {{code}} already uses this name", map[string]any{"code": code})
}

// createBdLeadForSupplier opens a purchase lead for a supplier whose BD
// company was just created. The lead starts at the first stage with the
// supplier's owner and those of its models the BD summary accepts
// (knownModels is the sorted system model list), so it stays editable there.
func createBdLeadForSupplier(tx *gorm.DB, s *Supplier, company *BdCompany, models []SupplierModel, knownModels []string, operatorID int) error {
	now := common.GetTimestamp()
	lead := BdLead{
		Side: BdSideBuy, Code: company.Code, Company: bdClip(company.Name, 128), Stage: BdStages[0],
		OwnerId: s.OwnerId, OwnerName: bdClip(s.Owner, 64),
		CreatedBy: operatorID, UpdatedBy: operatorID, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Create(&lead).Error; err != nil {
		return err
	}
	rows := make([]BdLeadModel, 0, len(models))
	seen := map[string]bool{}
	for _, m := range models {
		modelName := bdClip(cleanBdModel(m.ModelName), 128)
		class := bdClip(m.Variant, 64)
		key := bdModelIdentity(modelName, class)
		if BdModelKey(modelName) == "" || seen[key] || !bdLeadAcceptsModel(knownModels, modelName) || len(rows) >= 100 {
			continue
		}
		seen[key] = true
		var rate *float64
		if bdValidRate(m.DiscountPct) {
			rate = m.DiscountPct
		}
		rows = append(rows, BdLeadModel{LeadId: lead.Id, Model: modelName, ModelClass: class, PriceRate: rate, Sort: len(rows)})
	}
	if len(rows) == 0 {
		return nil
	}
	return tx.Create(&rows).Error
}

// bdLeadAcceptsModel mirrors SaveBdLead's model check: a name from the
// system model list, or a "<family>-all" name.
func bdLeadAcceptsModel(knownModels []string, name string) bool {
	if strings.HasSuffix(name, "-all") {
		return bdModelFamilyPattern.MatchString(name)
	}
	_, ok := slices.BinarySearch(knownModels, name)
	return ok
}

// ensureSupplierForBdLead creates a supplier record for a new purchase lead
// unless one already holds the lead's company code or has the company as
// its short or legal name. The record takes the company code, the lead's
// owner (when still an admin) and models, the supply price becoming the
// discount.
func ensureSupplierForBdLead(tx *gorm.DB, l *BdLead, operatorID int) error {
	if l.Side != BdSideBuy {
		return nil
	}
	name := strings.TrimSpace(l.Company)
	key := strings.ToLower(name)
	if key == "" {
		return nil
	}
	var existing int64
	err := tx.Model(&Supplier{}).
		Where("code = ? OR LOWER(TRIM(legal_name)) = ? OR LOWER(TRIM(short_name)) = ?", l.Code, key, key).
		Count(&existing).Error
	if err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	s := &Supplier{LegalName: bdClip(name, 200), ShortName: bdClip(name, 64), OwnerId: l.OwnerId}
	if err := s.normalize(); err != nil {
		return err
	}
	if err := resolveSupplierOwner(tx, s); err != nil {
		// The lead's owner may no longer be an admin; leave the record unassigned.
		var invalid *common.Message
		if !errors.As(err, &invalid) {
			return err
		}
		s.OwnerId, s.Owner = 0, ""
	}
	models := make([]SupplierModel, 0, len(l.Models))
	for _, m := range l.Models {
		row := SupplierModel{ModelName: m.Model, Variant: m.ModelClass, Enabled: true}
		if m.PriceRate != nil && *m.PriceRate > 0 {
			row.DiscountPct = m.PriceRate
		}
		models = append(models, row)
	}
	normalized, err := normalizeSupplierModels(models)
	if err != nil {
		return err
	}
	return insertSupplier(tx, s, normalized, l.Code, operatorID)
}

// backfillSupplierCodes moves records numbered before codes were shared
// with the BD summary (V001…) onto their BD supplier company code, creating
// the company and its lead the way a new record does. A record whose
// company code another record already holds keeps its old code.
func backfillSupplierCodes() error {
	var rows []Supplier
	if err := DB.Where("code LIKE ?", "V%").Order("id").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	knownModels, err := supplierBdModelNames()
	if err != nil {
		return err
	}
	for i := range rows {
		s := &rows[i]
		var models []SupplierModel
		if err := DB.Where("supplier_id = ?", s.Id).Order("sort, id").Find(&models).Error; err != nil {
			return err
		}
		err := DB.Transaction(func(tx *gorm.DB) error {
			company, created, err := supplierBdCompany(tx, s)
			if err != nil || company == nil {
				return err
			}
			if err := checkSupplierCodeFree(tx, company.Code, s.Id); err != nil {
				var invalid *common.Message
				if errors.As(err, &invalid) {
					common.SysLog(fmt.Sprintf("supplier %s keeps its code: %s is held by another record", s.Code, company.Code))
					return nil
				}
				return err
			}
			if err := tx.Model(&Supplier{}).Where("id = ?", s.Id).Update("code", company.Code).Error; err != nil {
				return err
			}
			if !created {
				return nil
			}
			return createBdLeadForSupplier(tx, s, company, models, knownModels, s.CreatedBy)
		})
		if err != nil {
			return err
		}
	}
	return nil
}
