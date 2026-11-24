package controller

import (
	"errors"
	"io"
	"maps"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// Supplier audit actions render through the shared template table without
// editing the upstream file that declares it.
func init() {
	maps.Copy(auditContentTemplates, map[string]string{
		"supplier.create":               "Created supplier ${name} (${code})",
		"supplier.update":               "Updated supplier ${name} (${code})",
		"supplier.delete":               "Deleted supplier ${name} (${code})",
		"supplier.contract.create":      "Created supplier contract ${contract_no}",
		"supplier.contract.update":      "Updated supplier contract ${contract_no}",
		"supplier.contract.void":        "Voided supplier contract ${contract_no}",
		"supplier.contract.set_current": "Set ${contract_no} as the current supplier contract",
		"supplier.attachment.upload":    "Uploaded supplier attachment ${file_name}",
		"supplier.attachment.delete":    "Deleted supplier attachment ${file_name}",
		"supplier.channels.update":      "Changed the channels of supplier ${id}",
		"supplier.channels.pull_models": "Pulled ${count} models from channel ${channel_id} into supplier ${id}",
	})
}

type supplierRequest struct {
	model.Supplier
	Models []model.SupplierModel `json:"models"`
}

// supplierAPIError turns a missing row into a translatable message and passes
// other errors through.
func supplierAPIError(c *gin.Context, err error, notFoundKey string) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorT(c, notFoundKey)
		return
	}
	common.ApiError(c, err)
}

func supplierIDParam(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return 0, false
	}
	return id, true
}

func GetSuppliers(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	items, total, err := model.SearchSuppliers(c.Query("keyword"), c.Query("status"), pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(items)
	common.ApiSuccess(c, pageInfo)
}

func GetSupplierStats(c *gin.Context) {
	stats, err := model.GetSupplierStats()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"stats":                stats,
		"storage_enabled":      service.SupplierStorageEnabled(),
		"attachment_max_bytes": service.SupplierAttachmentMaxBytes(),
	})
}

func GetSupplier(c *gin.Context) {
	id, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	detail, err := model.GetSupplierDetail(id)
	if err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	common.ApiSuccess(c, detail)
}

func CreateSupplier(c *gin.Context) {
	var req supplierRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	created, err := model.CreateSupplier(&req.Supplier, req.Models, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "supplier.create", map[string]any{"id": created.Id, "code": created.Code, "name": created.ShortName})
	detail, err := model.GetSupplierDetail(created.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func UpdateSupplier(c *gin.Context) {
	id, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	var req supplierRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	before, after, err := model.UpdateSupplier(id, &req.Supplier, req.Models, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	recordManageAudit(c, "supplier.update", map[string]any{
		"id":          id,
		"code":        after.Code,
		"name":        after.ShortName,
		"changes":     model.SupplierAuditChanges(before, after),
		"model_count": len(req.Models),
	})
	detail, err := model.GetSupplierDetail(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, detail)
}

func DeleteSupplier(c *gin.Context) {
	id, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	deleted, err := model.DeleteSupplier(id)
	if err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	recordManageAudit(c, "supplier.delete", map[string]any{"id": id, "code": deleted.Code, "name": deleted.ShortName})
	common.ApiSuccess(c, nil)
}

func CreateSupplierContract(c *gin.Context) {
	supplierID, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	var input model.SupplierContract
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	created, err := model.CreateSupplierContract(supplierID, &input, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	recordManageAudit(c, "supplier.contract.create", map[string]any{"id": created.Id, "supplier_id": supplierID, "contract_no": created.ContractNo})
	common.ApiSuccess(c, created)
}

func UpdateSupplierContract(c *gin.Context) {
	id, ok := supplierIDParam(c, "contract_id")
	if !ok {
		return
	}
	var input model.SupplierContract
	if err := common.DecodeJson(c.Request.Body, &input); err != nil {
		common.ApiError(c, err)
		return
	}
	before, after, err := model.UpdateSupplierContract(id, &input, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Contract not found")
		return
	}
	recordManageAudit(c, "supplier.contract.update", map[string]any{"id": id, "supplier_id": after.SupplierId, "contract_no": after.ContractNo, "changes": model.SupplierAuditChanges(before, after)})
	common.ApiSuccess(c, after)
}

func VoidSupplierContract(c *gin.Context) {
	id, ok := supplierIDParam(c, "contract_id")
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if strings.TrimSpace(req.Reason) == "" || utf8.RuneCountInString(req.Reason) > 255 {
		common.ApiErrorT(c, "Enter a reason of at most 255 characters")
		return
	}
	voided, err := model.VoidSupplierContract(id, req.Reason, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Contract not found")
		return
	}
	recordManageAudit(c, "supplier.contract.void", map[string]any{"id": id, "supplier_id": voided.SupplierId, "contract_no": voided.ContractNo, "reason": voided.VoidReason})
	common.ApiSuccess(c, voided)
}

func SetCurrentSupplierContract(c *gin.Context) {
	id, ok := supplierIDParam(c, "contract_id")
	if !ok {
		return
	}
	current, err := model.SetCurrentSupplierContract(id, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Contract not found")
		return
	}
	recordManageAudit(c, "supplier.contract.set_current", map[string]any{"id": id, "supplier_id": current.SupplierId, "contract_no": current.ContractNo})
	common.ApiSuccess(c, current)
}

func GetSupplierFrameworks(c *gin.Context) {
	supplierID, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	frameworks, err := model.ListSupplierFrameworks(supplierID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, frameworks)
}

func PreviewSupplierContractNo(c *gin.Context) {
	supplierID, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	parentID, _ := strconv.Atoi(c.Query("parent_contract_id"))
	contractNo, err := model.PreviewSupplierContractNo(supplierID, c.Query("business_line"), c.Query("contract_type"), parentID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"contract_no": contractNo})
}

func GetSupplierModelCatalog(c *gin.Context) {
	days, _ := strconv.Atoi(c.DefaultQuery("days", "30"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	items, err := model.SupplierModelCatalog(c.Query("keyword"), min(max(days, 1), 365), min(max(limit, 1), 100))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func RecordSupplierModelSearch(c *gin.Context) {
	var req struct {
		ModelName string `json:"model_name"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || utf8.RuneCountInString(req.ModelName) > 128 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.RecordSupplierModelSearch(c.GetInt("id"), req.ModelName); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

func CompareSupplierModel(c *gin.Context) {
	offers, err := model.CompareSupplierModel(c.Query("model_name"), c.Query("sort"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, offers)
}

// UploadSupplierAttachment stores a due diligence report or contract file.
// The whole file is read into memory to sign its hash, so the size limit is
// enforced on the request body first.
func UploadSupplierAttachment(c *gin.Context) {
	maxBytes := service.SupplierAttachmentMaxBytes()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes+(1<<20))
	// FormFile parses the multipart body first, so an oversized upload is
	// reported as such rather than as missing form fields.
	header, err := c.FormFile("file")
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			common.ApiErrorT(c, "The file is larger than {{size}} MB", map[string]any{"size": maxBytes >> 20})
			return
		}
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if header.Size > maxBytes {
		common.ApiErrorT(c, "The file is larger than {{size}} MB", map[string]any{"size": maxBytes >> 20})
		return
	}
	bizType := c.PostForm("biz_type")
	bizID, _ := strconv.Atoi(c.PostForm("biz_id"))
	if !model.ValidSupplierAttachmentBizType(bizType) || bizID <= 0 {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	ext := strings.ToLower(filepath.Ext(header.Filename))
	contentType, allowed := service.SupplierAttachmentTypes[ext]
	if !allowed {
		common.ApiErrorT(c, "Only PDF, Word, Excel, PNG, JPG and ZIP files can be uploaded")
		return
	}
	if _, err := model.SupplierIdOfAttachmentTarget(bizType, bizID); err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	file, err := header.Open()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if int64(len(body)) > maxBytes {
		common.ApiErrorT(c, "The file is larger than {{size}} MB", map[string]any{"size": maxBytes >> 20})
		return
	}
	key, err := service.NewSupplierObjectKey(bizType, bizID, ext)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := service.PutSupplierObject(c.Request.Context(), key, contentType, body); err != nil {
		common.ApiError(c, err)
		return
	}
	fileName := filepath.Base(strings.ReplaceAll(header.Filename, "\\", "/"))
	if utf8.RuneCountInString(fileName) > 200 {
		fileName = string([]rune(fileName)[:200])
	}
	attachment := &model.SupplierAttachment{
		BizType:     bizType,
		BizId:       bizID,
		FileName:    fileName,
		ObjectKey:   key,
		ContentType: contentType,
		Size:        int64(len(body)),
		CreatedBy:   c.GetInt("id"),
	}
	if err := model.CreateSupplierAttachment(attachment); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "supplier.attachment.upload", map[string]any{"id": attachment.Id, "biz_type": bizType, "biz_id": bizID, "file_name": fileName})
	common.ApiSuccess(c, attachment)
}

func GetSupplierAttachmentURL(c *gin.Context) {
	id, ok := supplierIDParam(c, "attachment_id")
	if !ok {
		return
	}
	attachment, err := model.GetSupplierAttachment(id)
	if err != nil {
		supplierAPIError(c, err, "Attachment not found")
		return
	}
	url, err := service.PresignSupplierObject(c.Request.Context(), attachment.ObjectKey, attachment.FileName, 5*time.Minute)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"url": url})
}

func DeleteSupplierAttachment(c *gin.Context) {
	id, ok := supplierIDParam(c, "attachment_id")
	if !ok {
		return
	}
	attachment, err := model.GetSupplierAttachment(id)
	if err != nil {
		supplierAPIError(c, err, "Attachment not found")
		return
	}
	if err := model.DeleteSupplierAttachment(id); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "supplier.attachment.delete", map[string]any{"id": id, "biz_type": attachment.BizType, "biz_id": attachment.BizId, "file_name": attachment.FileName})
	common.ApiSuccess(c, nil)
}

type supplierOwner struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// GetSupplierMeta returns the choices the supplier forms offer: owners
// (admins who can view suppliers), the system model list, variant notes
// already in use, and channels with the supplier each is bound to.
func GetSupplierMeta(c *gin.Context) {
	users, err := model.ListBdOwnerCandidates()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	owners := make([]supplierOwner, 0, len(users))
	for i := range users {
		if authz.Can(users[i].Id, users[i].Role, authz.SupplierRead) {
			owners = append(owners, supplierOwner{Id: users[i].Id, Name: model.BdUserName(&users[i])})
		}
	}
	models, err := model.ListBdModelNames()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	variants, err := model.SupplierModelVariants()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channels, err := model.ListSupplierChannelOptions()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"owners": owners, "models": models, "variants": variants, "channels": channels})
}

// MatchSuppliers looks suppliers up by name for duplicate checks.
func MatchSuppliers(c *gin.Context) {
	matches, err := model.MatchSuppliers(c.Query("name"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, matches)
}

// GetSupplierChannelOffers returns the supplier terms for the models of the
// given channels (?channel_ids=1,2,3).
func GetSupplierChannelOffers(c *gin.Context) {
	ids := make([]int, 0)
	for raw := range strings.SplitSeq(c.Query("channel_ids"), ",") {
		if id, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	if len(ids) > 500 {
		ids = ids[:500]
	}
	offers, err := model.SupplierChannelOffers(ids)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, offers)
}

func SetSupplierChannels(c *gin.Context) {
	supplierID, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	var req struct {
		ChannelIds []int `json:"channel_ids"`
	}
	if err := common.DecodeJson(c.Request.Body, &req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := model.SetSupplierChannels(supplierID, req.ChannelIds, c.GetInt("id")); err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	recordManageAudit(c, "supplier.channels.update", map[string]any{"id": supplierID, "channel_ids": req.ChannelIds})
	common.ApiSuccess(c, nil)
}

func PullSupplierChannelModels(c *gin.Context) {
	supplierID, ok := supplierIDParam(c, "id")
	if !ok {
		return
	}
	channelID, ok := supplierIDParam(c, "channel_id")
	if !ok {
		return
	}
	added, err := model.PullSupplierChannelModels(supplierID, channelID, c.GetInt("id"))
	if err != nil {
		supplierAPIError(c, err, "Supplier not found")
		return
	}
	recordManageAudit(c, "supplier.channels.pull_models", map[string]any{"id": supplierID, "channel_id": channelID, "count": added})
	common.ApiSuccess(c, gin.H{"added": added})
}
