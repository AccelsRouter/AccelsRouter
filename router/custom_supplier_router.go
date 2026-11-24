package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// registerSupplierRoutes mounts supplier management. Every route needs an
// admin session plus an explicit supplier permission granted by Root.
func registerSupplierRoutes(apiRouter *gin.RouterGroup) {
	supplierRoute := apiRouter.Group("/admin/suppliers")
	supplierRoute.Use(middleware.AdminAuth())
	for _, route := range supplierPermissionRoutes {
		handlePermissionRoute(supplierRoute, route.method, route.path, route.permission, route.handler)
	}
}

var supplierPermissionRoutes = []permissionRoute{
	{method: http.MethodGet, path: "/", permission: authz.SupplierRead, handler: controller.GetSuppliers},
	{method: http.MethodPost, path: "/", permission: authz.SupplierWrite, handler: controller.CreateSupplier},
	{method: http.MethodGet, path: "/stats", permission: authz.SupplierRead, handler: controller.GetSupplierStats},
	{method: http.MethodGet, path: "/meta", permission: authz.SupplierRead, handler: controller.GetSupplierMeta},
	{method: http.MethodGet, path: "/match", permission: authz.SupplierRead, handler: controller.MatchSuppliers},
	{method: http.MethodGet, path: "/channel-offers", permission: authz.SupplierRead, handler: controller.GetSupplierChannelOffers},
	{method: http.MethodGet, path: "/models/catalog", permission: authz.SupplierRead, handler: controller.GetSupplierModelCatalog},
	{method: http.MethodGet, path: "/models/compare", permission: authz.SupplierRead, handler: controller.CompareSupplierModel},
	{method: http.MethodPost, path: "/models/search-log", permission: authz.SupplierRead, handler: controller.RecordSupplierModelSearch},
	{method: http.MethodPut, path: "/contracts/:contract_id", permission: authz.SupplierWrite, handler: controller.UpdateSupplierContract},
	{method: http.MethodPost, path: "/contracts/:contract_id/void", permission: authz.SupplierWrite, handler: controller.VoidSupplierContract},
	{method: http.MethodPost, path: "/contracts/:contract_id/current", permission: authz.SupplierWrite, handler: controller.SetCurrentSupplierContract},
	{method: http.MethodPost, path: "/attachments", permission: authz.SupplierWrite, handler: controller.UploadSupplierAttachment},
	{method: http.MethodGet, path: "/attachments/:attachment_id/download", permission: authz.SupplierRead, handler: controller.GetSupplierAttachmentURL},
	{method: http.MethodDelete, path: "/attachments/:attachment_id", permission: authz.SupplierWrite, handler: controller.DeleteSupplierAttachment},
	{method: http.MethodGet, path: "/:id", permission: authz.SupplierRead, handler: controller.GetSupplier},
	{method: http.MethodPut, path: "/:id", permission: authz.SupplierWrite, handler: controller.UpdateSupplier},
	{method: http.MethodDelete, path: "/:id", permission: authz.SupplierWrite, handler: controller.DeleteSupplier},
	{method: http.MethodPost, path: "/:id/contracts", permission: authz.SupplierWrite, handler: controller.CreateSupplierContract},
	{method: http.MethodGet, path: "/:id/contracts/next-no", permission: authz.SupplierWrite, handler: controller.PreviewSupplierContractNo},
	{method: http.MethodGet, path: "/:id/frameworks", permission: authz.SupplierRead, handler: controller.GetSupplierFrameworks},
	{method: http.MethodPut, path: "/:id/channels", permission: authz.SupplierWrite, handler: controller.SetSupplierChannels},
	{method: http.MethodPost, path: "/:id/channels/:channel_id/pull-models", permission: authz.SupplierWrite, handler: controller.PullSupplierChannelModels},
}
