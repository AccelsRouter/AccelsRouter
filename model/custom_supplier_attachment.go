package model

import (
	"slices"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	SupplierAttachmentDueDiligence = "supplier_dd"
	SupplierAttachmentContract     = "supplier_contract"
)

// SupplierAttachment is a file kept in object storage. ObjectKey never leaves
// the server; downloads go through short-lived presigned URLs.
type SupplierAttachment struct {
	Id          int            `json:"id"`
	BizType     string         `json:"biz_type" gorm:"type:varchar(32);index:idx_supplier_attachment_biz,priority:1"`
	BizId       int            `json:"biz_id" gorm:"index:idx_supplier_attachment_biz,priority:2"`
	FileName    string         `json:"file_name" gorm:"type:varchar(255)"`
	ObjectKey   string         `json:"-" gorm:"type:varchar(512)"`
	ContentType string         `json:"content_type" gorm:"type:varchar(128)"`
	Size        int64          `json:"size" gorm:"type:bigint"`
	CreatedBy   int            `json:"created_by"`
	CreatedTime int64          `json:"created_time" gorm:"type:bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func ValidSupplierAttachmentBizType(bizType string) bool {
	return slices.Contains([]string{SupplierAttachmentDueDiligence, SupplierAttachmentContract}, bizType)
}

// SupplierIdOfAttachmentTarget resolves the supplier that owns an attachment
// target and fails when the target does not exist.
func SupplierIdOfAttachmentTarget(bizType string, bizID int) (int, error) {
	if bizType == SupplierAttachmentContract {
		contract, err := GetSupplierContract(bizID)
		if err != nil {
			return 0, err
		}
		return contract.SupplierId, nil
	}
	var s Supplier
	if err := DB.Select("id").First(&s, bizID).Error; err != nil {
		return 0, err
	}
	return s.Id, nil
}

func CreateSupplierAttachment(a *SupplierAttachment) error {
	a.Id = 0
	a.CreatedTime = common.GetTimestamp()
	return DB.Create(a).Error
}

func GetSupplierAttachment(id int) (*SupplierAttachment, error) {
	var a SupplierAttachment
	if err := DB.First(&a, id).Error; err != nil {
		return nil, err
	}
	return &a, nil
}

// DeleteSupplierAttachment hides the record; the stored object is kept so the
// audit trail can still be followed.
func DeleteSupplierAttachment(id int) error {
	return DB.Delete(&SupplierAttachment{}, id).Error
}

func listSupplierAttachments(supplierID int, contractIDs []int) ([]SupplierAttachment, error) {
	attachments := make([]SupplierAttachment, 0)
	targets := DB.Where("biz_type = ? AND biz_id = ?", SupplierAttachmentDueDiligence, supplierID)
	if len(contractIDs) > 0 {
		targets = targets.Or("biz_type = ? AND biz_id IN ?", SupplierAttachmentContract, contractIDs)
	}
	err := DB.Where(targets).Order("id desc").Find(&attachments).Error
	return attachments, err
}
