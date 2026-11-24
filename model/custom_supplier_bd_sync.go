package model

import (
	"errors"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// Supplier records and BD purchase leads name the same companies. Creating
// either one creates the other with the same name when it does not exist
// yet, inside the same transaction. Only creation is mirrored; later edits
// stay on their own side.

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

// ensureBdLeadForSupplier opens a purchase lead for a new supplier unless a
// BD supplier company already carries its short or legal name. The lead
// starts at the first stage with the supplier's owner and those of its
// models the BD summary accepts (knownModels is the sorted system model
// list), so the lead stays editable there.
func ensureBdLeadForSupplier(tx *gorm.DB, s *Supplier, models []SupplierModel, knownModels []string, operatorID int) error {
	name := supplierDisplayName(s)
	if name == "" {
		return nil
	}
	keys := []string{bdNameKey(s.ShortName), bdNameKey(s.LegalName)}
	var existing int64
	if err := tx.Model(&BdCompany{}).Where("side = ? AND name_key IN ?", BdSideBuy, keys).Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	company, err := EnsureBdCompany(tx, BdSideBuy, name)
	if err != nil {
		return err
	}
	now := common.GetTimestamp()
	lead := BdLead{
		Side: BdSideBuy, Code: company.Code, Company: bdClip(name, 128), Stage: BdStages[0],
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

// ensureSupplierForBdLead creates a supplier record for a new purchase lead
// unless a supplier already has the lead's company as its short or legal
// name. The record is named after the company and takes the lead's owner
// (when still an admin) and models, the supply price becoming the discount.
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
	err := tx.Model(&Supplier{}).Where("LOWER(TRIM(legal_name)) = ? OR LOWER(TRIM(short_name)) = ?", key, key).Count(&existing).Error
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
	return insertSupplier(tx, s, normalized, operatorID)
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
