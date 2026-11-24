package model

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// SupplierChannel binds a gateway channel to the supplier that provides it.
// A channel belongs to at most one supplier.
type SupplierChannel struct {
	Id          int   `json:"id"`
	SupplierId  int   `json:"supplier_id" gorm:"index"`
	ChannelId   int   `json:"channel_id" gorm:"uniqueIndex"`
	CreatedTime int64 `json:"created_time" gorm:"type:bigint"`
}

// SupplierChannelView is a bound channel as shown on the supplier page.
type SupplierChannelView struct {
	ChannelId  int    `json:"channel_id"`
	Name       string `json:"name"`
	Type       int    `json:"type"`
	Status     int    `json:"status"`
	ModelCount int    `json:"model_count"`
}

// SupplierChannelOption is a channel offered for binding, with the supplier
// it is already bound to (0 when free).
type SupplierChannelOption struct {
	ChannelId  int    `json:"channel_id"`
	Name       string `json:"name"`
	Status     int    `json:"status"`
	SupplierId int    `json:"supplier_id"`
}

// SupplierChannelOffer is one model of a bound channel with the supplier's
// terms for it, shown when admins bind customers to channels.
type SupplierChannelOffer struct {
	ChannelId    int      `json:"channel_id"`
	SupplierId   int      `json:"supplier_id"`
	SupplierName string   `json:"supplier_name"`
	ModelName    string   `json:"model_name"`
	Variant      string   `json:"variant"`
	Source       string   `json:"source"`
	DiscountPct  *float64 `json:"discount_pct"`
}

func channelModelNames(models string) []string {
	names := make([]string, 0)
	for name := range strings.SplitSeq(models, ",") {
		name = strings.TrimSpace(name)
		if name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

func listSupplierChannels(supplierID int) ([]SupplierChannelView, error) {
	var bindings []SupplierChannel
	if err := DB.Where("supplier_id = ?", supplierID).Order("id").Find(&bindings).Error; err != nil {
		return nil, err
	}
	views := make([]SupplierChannelView, 0, len(bindings))
	if len(bindings) == 0 {
		return views, nil
	}
	ids := make([]int, 0, len(bindings))
	for _, b := range bindings {
		ids = append(ids, b.ChannelId)
	}
	var channels []Channel
	if err := DB.Select("id", "name", "type", "status", "models").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	byID := make(map[int]Channel, len(channels))
	for _, ch := range channels {
		byID[ch.Id] = ch
	}
	for _, id := range ids {
		ch, ok := byID[id]
		if !ok {
			// The channel was deleted; keep the binding visible so it can be removed.
			views = append(views, SupplierChannelView{ChannelId: id})
			continue
		}
		views = append(views, SupplierChannelView{ChannelId: id, Name: ch.Name, Type: ch.Type, Status: ch.Status, ModelCount: len(channelModelNames(ch.Models))})
	}
	return views, nil
}

// ListSupplierChannelOptions lists every channel with the supplier it is
// bound to, for the binding picker.
func ListSupplierChannelOptions() ([]SupplierChannelOption, error) {
	var channels []Channel
	if err := DB.Select("id", "name", "status").Order("id desc").Find(&channels).Error; err != nil {
		return nil, err
	}
	var bindings []SupplierChannel
	if err := DB.Find(&bindings).Error; err != nil {
		return nil, err
	}
	owner := make(map[int]int, len(bindings))
	for _, b := range bindings {
		owner[b.ChannelId] = b.SupplierId
	}
	options := make([]SupplierChannelOption, 0, len(channels))
	for _, ch := range channels {
		options = append(options, SupplierChannelOption{ChannelId: ch.Id, Name: ch.Name, Status: ch.Status, SupplierId: owner[ch.Id]})
	}
	return options, nil
}

// SetSupplierChannels replaces the channels bound to a supplier. A channel
// already bound to another supplier is refused. Model rows pulled from a
// channel that is unbound keep their data but lose the channel link.
func SetSupplierChannels(supplierID int, channelIDs []int, operatorID int) error {
	ids := slices.Compact(slices.Sorted(slices.Values(channelIDs)))
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := touchSupplier(tx, supplierID, operatorID); err != nil {
			return err
		}
		if len(ids) > 0 {
			var found int64
			if err := tx.Model(&Channel{}).Where("id IN ?", ids).Count(&found).Error; err != nil {
				return err
			}
			if int(found) != len(ids) {
				return supplierInvalid("Channel not found")
			}
			var taken []SupplierChannel
			if err := tx.Where("channel_id IN ? AND supplier_id <> ?", ids, supplierID).Find(&taken).Error; err != nil {
				return err
			}
			if len(taken) > 0 {
				return supplierInvalid("Channel {{id}} is already bound to another supplier", map[string]any{"id": taken[0].ChannelId})
			}
		}
		var current []SupplierChannel
		if err := tx.Where("supplier_id = ?", supplierID).Find(&current).Error; err != nil {
			return err
		}
		removed := make([]int, 0)
		for _, b := range current {
			if !slices.Contains(ids, b.ChannelId) {
				removed = append(removed, b.ChannelId)
			}
		}
		if len(removed) > 0 {
			if err := tx.Where("supplier_id = ? AND channel_id IN ?", supplierID, removed).Delete(&SupplierChannel{}).Error; err != nil {
				return err
			}
			if err := tx.Model(&SupplierModel{}).Where("supplier_id = ? AND channel_id IN ?", supplierID, removed).Update("channel_id", 0).Error; err != nil {
				return err
			}
		}
		now := common.GetTimestamp()
		for _, id := range ids {
			if slices.ContainsFunc(current, func(b SupplierChannel) bool { return b.ChannelId == id }) {
				continue
			}
			if err := tx.Create(&SupplierChannel{SupplierId: supplierID, ChannelId: id, CreatedTime: now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// PullSupplierChannelModels adds the models of a bound channel that the
// supplier does not list for that channel yet, and returns how many it
// added. A row typed in by hand with the same model name is linked to the
// channel instead of duplicated. Terms such as the discount stay to be
// filled in, so the record shows them as missing.
func PullSupplierChannelModels(supplierID, channelID, operatorID int) (int, error) {
	added := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := touchSupplier(tx, supplierID, operatorID); err != nil {
			return err
		}
		var binding SupplierChannel
		if err := tx.Where("supplier_id = ? AND channel_id = ?", supplierID, channelID).First(&binding).Error; err != nil {
			return supplierInvalid("Bind the channel to this supplier first")
		}
		var channel Channel
		if err := tx.Select("id", "models").First(&channel, channelID).Error; err != nil {
			return err
		}
		var rows []SupplierModel
		if err := tx.Where("supplier_id = ?", supplierID).Order("sort").Find(&rows).Error; err != nil {
			return err
		}
		sort := len(rows)
		for _, name := range channelModelNames(channel.Models) {
			linked := slices.IndexFunc(rows, func(m SupplierModel) bool { return m.ModelName == name && m.ChannelId == channelID })
			if linked >= 0 {
				continue
			}
			typed := slices.IndexFunc(rows, func(m SupplierModel) bool { return m.ModelName == name && m.ChannelId == 0 })
			if typed >= 0 {
				if err := tx.Model(&SupplierModel{Id: rows[typed].Id}).Update("channel_id", channelID).Error; err != nil {
					return err
				}
				rows[typed].ChannelId = channelID
				continue
			}
			row := SupplierModel{SupplierId: supplierID, ModelName: name, Enabled: true, Sort: sort, ChannelId: channelID}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			rows = append(rows, row)
			sort++
			added++
		}
		return nil
	})
	return added, err
}

// SupplierChannelOffers returns, for the given channels, the terms of the
// supplier each one is bound to: rows pulled from that channel, and rows of
// the same supplier typed in by hand (no channel), keyed by model name.
func SupplierChannelOffers(channelIDs []int) ([]SupplierChannelOffer, error) {
	offers := make([]SupplierChannelOffer, 0)
	if len(channelIDs) == 0 {
		return offers, nil
	}
	var bindings []SupplierChannel
	if err := DB.Where("channel_id IN ?", channelIDs).Find(&bindings).Error; err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return offers, nil
	}
	supplierIDs := make([]int, 0, len(bindings))
	for _, b := range bindings {
		supplierIDs = append(supplierIDs, b.SupplierId)
	}
	var suppliers []Supplier
	if err := DB.Select("id", "short_name", "legal_name").Where("id IN ?", supplierIDs).Find(&suppliers).Error; err != nil {
		return nil, err
	}
	names := make(map[int]string, len(suppliers))
	for _, s := range suppliers {
		names[s.Id] = s.ShortName
		if names[s.Id] == "" {
			names[s.Id] = s.LegalName
		}
	}
	var rows []SupplierModel
	if err := DB.Where("supplier_id IN ? AND model_name <> ?", supplierIDs, "").Order("sort").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if _, live := names[b.SupplierId]; !live {
			continue // the supplier was deleted
		}
		for _, m := range rows {
			if m.SupplierId != b.SupplierId || (m.ChannelId != b.ChannelId && m.ChannelId != 0) {
				continue
			}
			offers = append(offers, SupplierChannelOffer{
				ChannelId: b.ChannelId, SupplierId: b.SupplierId, SupplierName: names[b.SupplierId],
				ModelName: m.ModelName, Variant: m.Variant, Source: m.Source, DiscountPct: m.DiscountPct,
			})
		}
	}
	return offers, nil
}

// SupplierBrief is a supplier found by name.
type SupplierBrief struct {
	Id        int    `json:"id"`
	Code      string `json:"code"`
	LegalName string `json:"legal_name"`
	ShortName string `json:"short_name"`
	Status    string `json:"status"`
	// Exact is true when the legal or short name equals the searched name,
	// ignoring case and surrounding spaces.
	Exact bool `json:"exact"`
}

// MatchSuppliers finds suppliers whose legal or short name contains name,
// exact matches first, so a new record or a BD lead can find an existing one.
func MatchSuppliers(name string) ([]SupplierBrief, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	matches := make([]SupplierBrief, 0)
	if key == "" {
		return matches, nil
	}
	like := "%" + key + "%"
	var suppliers []Supplier
	err := DB.Select("id", "code", "legal_name", "short_name", "status").
		Where("LOWER(legal_name) LIKE ? OR LOWER(short_name) LIKE ?", like, like).
		Order("id").Limit(20).Find(&suppliers).Error
	if err != nil {
		return nil, err
	}
	for _, s := range suppliers {
		exact := strings.ToLower(strings.TrimSpace(s.LegalName)) == key || strings.ToLower(strings.TrimSpace(s.ShortName)) == key
		matches = append(matches, SupplierBrief{Id: s.Id, Code: s.Code, LegalName: s.LegalName, ShortName: s.ShortName, Status: s.Status, Exact: exact})
	}
	slices.SortStableFunc(matches, func(a, b SupplierBrief) int {
		switch {
		case a.Exact && !b.Exact:
			return -1
		case b.Exact && !a.Exact:
			return 1
		}
		return 0
	})
	return matches, nil
}

// SupplierModelVariants lists the variant notes already used, as suggestions.
func SupplierModelVariants() ([]string, error) {
	var variants []string
	err := DB.Model(&SupplierModel{}).Where("variant <> ?", "").Distinct("variant").Order("variant").Pluck("variant", &variants).Error
	return variants, err
}
