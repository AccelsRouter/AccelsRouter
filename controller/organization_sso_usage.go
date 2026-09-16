// Fork-only org console + admin handlers for SSO domain auto-provisioning and
// usage reporting / invoice export. SSO domain mappings are admin-managed (the
// platform verifies domain ownership); the org console can only view them.
package controller

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/gin-gonic/gin"
)

// AdminListSsoProviders — GET /api/admin/organizations/sso-providers
// Lists the enabled OAuth providers an SSO domain mapping can bind to, so the
// admin UI can offer a picker instead of a free-text field.
func AdminListSsoProviders(c *gin.Context) {
	common.ApiSuccess(c, oauth.ListEnabledProviders())
}

// ---------------------------------------------------------------------------
// SSO domain mappings — admin-managed
// ---------------------------------------------------------------------------

// AdminListOrgSsoDomains — GET /api/admin/organizations/:id/sso-domains
func AdminListOrgSsoDomains(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	domains, err := model.ListOrgSsoDomains(orgId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, domains)
}

type ssoDomainRequest struct {
	Domain   string `json:"domain"`
	Provider string `json:"provider"` // OAuth provider (SSO connection) allowed to provision
}

// AdminAddOrgSsoDomain — POST /api/admin/organizations/:id/sso-domains
func AdminAddOrgSsoDomain(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	org, err := model.GetOrganizationById(orgId)
	if err != nil || org == nil {
		common.ApiErrorMsg(c, "organization not found")
		return
	}
	var req ssoDomainRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if model.NormalizeSsoDomain(req.Domain) == "" {
		common.ApiErrorMsg(c, "invalid domain")
		return
	}
	provider := strings.ToLower(strings.TrimSpace(req.Provider))
	if provider == "" {
		common.ApiErrorMsg(c, "provider is required")
		return
	}
	// The provider must be a currently enabled OAuth connection, otherwise the
	// mapping could never match a login and would silently never provision.
	if !oauth.IsProviderEnabled(provider) {
		common.ApiErrorMsg(c, "provider is not an enabled OAuth connection")
		return
	}
	rec, err := model.AddOrgSsoDomain(orgId, req.Domain, provider)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	model.RecordOrgAudit(orgId, c.GetInt("id"), "sso.domain.add", rec.Domain, "provider="+provider)
	common.ApiSuccess(c, rec)
}

// AdminDeleteOrgSsoDomain — DELETE /api/admin/organizations/:id/sso-domains/:domain_id
func AdminDeleteOrgSsoDomain(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	domainId, _ := strconv.Atoi(c.Param("domain_id"))
	if err := model.DeleteOrgSsoDomain(domainId, orgId); err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOrgAudit(orgId, c.GetInt("id"), "sso.domain.delete", fmt.Sprintf("domain:%d", domainId), "")
	common.ApiSuccess(c, nil)
}

// ListMyOrgSsoDomains — GET /api/organization/sso-domains (read-only for the org)
func ListMyOrgSsoDomains(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	domains, err := model.ListOrgSsoDomains(org.Id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, domains)
}

// ---------------------------------------------------------------------------
// Usage reporting + invoice export
// ---------------------------------------------------------------------------

const (
	usageDefaultSpan int64 = 30 * 24 * 3600  // 30 days
	usageMaxSpan     int64 = 366 * 24 * 3600 // 1 year
)

// parseUsageWindow parses ?from=&to= (unix seconds) into a bounded, ordered
// window and defaults sensibly: a missing end is "now"; a missing start is one
// default span before the end. It rejects an inverted range and any span wider
// than the max (so a report/invoice never scans an unbounded slice of the log
// DB). On rejection it writes the error and returns ok=false.
func parseUsageWindow(c *gin.Context) (from, to int64, ok bool) {
	from, _ = strconv.ParseInt(c.Query("from"), 10, 64)
	to, _ = strconv.ParseInt(c.Query("to"), 10, 64)
	now := time.Now().Unix()
	if to <= 0 {
		to = now
	}
	if from <= 0 {
		from = to - usageDefaultSpan
	}
	if from > to {
		common.ApiErrorMsg(c, "invalid range: from must not be after to")
		return 0, 0, false
	}
	if to-from > usageMaxSpan {
		common.ApiErrorMsg(c, "date range too large (max 1 year)")
		return 0, 0, false
	}
	return from, to, true
}

// GetMyOrgUsage — GET /api/organization/usage
func GetMyOrgUsage(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetOrgUsage(org.Id, from, to)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// ExportMyOrgUsage — GET /api/organization/usage/export (CSV invoice export)
func ExportMyOrgUsage(c *gin.Context) {
	org, _, ok := callerOrg(c)
	if !ok {
		return
	}
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetOrgUsage(org.Id, from, to)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	writeUsageCSV(c, org, report)
}

// AdminGetOrgUsage — GET /api/admin/organizations/:id/usage
func AdminGetOrgUsage(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	report, err := model.GetOrgUsage(orgId, from, to)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

// AdminListOrgLogs — GET /api/admin/organizations/:id/logs
// Per-request call records for one org, with the reseller retail discount
// overlaid per row (ListOrgLogs) so a platform admin can see the discounted
// price a reseller customer actually paid.
func AdminListOrgLogs(c *gin.Context) {
	orgId, _ := strconv.Atoi(c.Param("id"))
	from, to, ok := parseUsageWindow(c)
	if !ok {
		return
	}
	page := common.GetPageQuery(c)
	logs, total, err := model.ListOrgLogs(orgId, from, to, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(logs)
	common.ApiSuccess(c, page)
}

// writeUsageCSV streams the report as a CSV invoice. Quota is rendered in the
// platform's display unit via common.LogQuota-style conversion so the exported
// numbers match the console.
func writeUsageCSV(c *gin.Context, org *model.Organization, report *model.OrgUsageReport) {
	filename := fmt.Sprintf("usage-%d-%d-%d.csv", org.Id, report.From, report.To)
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")

	w := csv.NewWriter(c.Writer)
	defer w.Flush()

	_ = w.Write([]string{"Organization", csvSafe(org.Name)})
	_ = w.Write([]string{"From (unix)", strconv.FormatInt(report.From, 10)})
	_ = w.Write([]string{"To (unix)", strconv.FormatInt(report.To, 10)})
	_ = w.Write([]string{"Total quota", strconv.FormatInt(report.TotalQuota, 10)})
	_ = w.Write([]string{"Total requests", strconv.FormatInt(report.TotalRequests, 10)})
	_ = w.Write([]string{"Total prompt tokens", strconv.FormatInt(report.TotalPrompt, 10)})
	_ = w.Write([]string{"Total completion tokens", strconv.FormatInt(report.TotalCompletion, 10)})
	_ = w.Write(nil)

	writeSection := func(title string, buckets []model.OrgUsageBucket) {
		_ = w.Write([]string{title, "Quota", "Requests", "Prompt tokens", "Completion tokens"})
		for _, b := range buckets {
			_ = w.Write([]string{
				csvSafe(b.Key),
				strconv.FormatInt(b.Quota, 10),
				strconv.FormatInt(b.Requests, 10),
				strconv.FormatInt(b.PromptTokens, 10),
				strconv.FormatInt(b.CompletionTokens, 10),
			})
		}
		_ = w.Write(nil)
	}
	writeSection("By workspace", report.ByWorkspace)
	writeSection("By model", report.ByModel)
	writeSection("By member", report.ByMember)
}

// csvSafe neutralizes CSV formula (macro) injection: a cell beginning with a
// formula trigger is prefixed with a single quote so spreadsheet apps treat it
// as text. Member labels are downstream usernames (a different trust boundary
// from the admin who opens the invoice), and usernames have no charset limit.
func csvSafe(s string) string {
	if s == "" {
		return s
	}
	switch s[0] {
	case '=', '+', '-', '@', '\t', '\r':
		return "'" + s
	}
	return s
}
