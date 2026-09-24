package model

// Fork-only SSO auto-provisioning (JIT). An OrgSsoDomain maps an email domain
// to the organization that should automatically claim its members on OAuth/OIDC
// login. This is the practical "SSO auto-provision" flow: when a user signs in
// through the corporate IdP, they are attached to the org without an admin
// creating the account by hand.
//
// Security model — domain mappings are ADMIN-MANAGED only. An org may not
// self-claim a domain, because claiming a public domain (gmail.com, ...) would
// conscript unrelated users into the org's roster. The platform admin adds a
// mapping after verifying domain ownership out of band, exactly like a real SSO
// setup. Auto-provision also NEVER moves an existing payer: if the user already
// has an OrgAccount (UNIQUE user_id single-payer), the mapping is ignored.
//
// Each mapping is bound to a specific OAuth provider (the corporate SSO
// connection). Auto-provision fires ONLY when the login came through that exact
// provider — never merely because the email string ends in the domain. This is
// the load-bearing control: without it, a user authenticating through any
// enabled provider that returns a self-asserted, unverified email (public
// GitHub OAuth, a loose generic OIDC) could present "x@acme.com" and get billed
// to Acme's wallet. Matching on the provider means only the IdP that actually
// authenticates membership in the domain can provision.

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// OrgSsoDomain maps a verified email domain to an org for JIT provisioning,
// bound to the OAuth provider (SSO connection) allowed to provision it.
type OrgSsoDomain struct {
	Id          int    `json:"id" gorm:"primarykey"`
	OrgId       int    `json:"org_id" gorm:"index;not null"`
	Domain      string `json:"domain" gorm:"type:varchar(128);uniqueIndex;not null"` // lowercased, no '@'
	Provider    string `json:"provider" gorm:"type:varchar(64);not null"`            // OAuth provider key (e.g. "oidc")
	CreatedTime int64  `json:"created_time"`
}

// emailDomain returns the lowercased domain part of an email, or "" when the
// address has no single '@'.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 || at == len(email)-1 || strings.Count(email, "@") != 1 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

// NormalizeSsoDomain accepts a raw domain or an "@domain" / email-like string
// and returns the bare lowercased domain, or "" when it is not a usable domain.
func NormalizeSsoDomain(raw string) string {
	d := strings.ToLower(strings.TrimSpace(raw))
	d = strings.TrimPrefix(d, "@")
	if d == "" || strings.ContainsAny(d, " @/\\") || !strings.Contains(d, ".") {
		return ""
	}
	return d
}

func AddOrgSsoDomain(orgId int, rawDomain, provider string) (*OrgSsoDomain, error) {
	domain := NormalizeSsoDomain(rawDomain)
	if domain == "" {
		return nil, errors.New("invalid domain")
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return nil, errors.New("provider is required")
	}
	rec := &OrgSsoDomain{OrgId: orgId, Domain: domain, Provider: provider, CreatedTime: common.GetTimestamp()}
	if err := DB.Create(rec).Error; err != nil {
		return nil, err
	}
	return rec, nil
}

func ListOrgSsoDomains(orgId int) ([]*OrgSsoDomain, error) {
	var out []*OrgSsoDomain
	err := DB.Where("org_id = ?", orgId).Order("id ASC").Find(&out).Error
	return out, err
}

// DeleteOrgSsoDomain removes a mapping, scoped to its owning org so one org can
// never delete another's mapping.
func DeleteOrgSsoDomain(id, orgId int) error {
	return DB.Where("id = ? AND org_id = ?", id, orgId).Delete(&OrgSsoDomain{}).Error
}

func getOrgSsoDomain(domain string) (*OrgSsoDomain, error) {
	var rec OrgSsoDomain
	err := DB.Where("domain = ?", domain).First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// AutoProvisionOrgMembership attaches the user to the org mapped to its email
// domain, if any, but ONLY when the login came through the provider that
// mapping is bound to. It is best-effort and idempotent: no email, no mapping,
// a provider mismatch, an inactive org, or a user who is already in an org all
// return (false, nil). It is safe to call on every login. It never moves an
// existing payer, preserving the UNIQUE(user_id) single-payer invariant.
func AutoProvisionOrgMembership(user *User, provider string) (bool, error) {
	if user == nil || user.Email == "" {
		return false, nil
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return false, nil
	}
	domain := emailDomain(user.Email)
	if domain == "" {
		return false, nil
	}
	mapping, err := getOrgSsoDomain(domain)
	if err != nil || mapping == nil {
		return false, err
	}
	// The login must come through the mapping's designated SSO provider; a
	// self-asserted email from any other provider must never provision.
	if mapping.Provider != provider {
		return false, nil
	}
	// Respect single-payer: never override an existing binding.
	existing, err := GetOrgAccountByUser(user.Id)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	org, err := GetOrganizationById(mapping.OrgId)
	if err != nil || org == nil || org.Status != OrgStatusActive {
		return false, err
	}
	err = AttachOrgAccount(&OrgAccount{
		OrgId:    org.Id,
		UserId:   user.Id,
		Relation: OrgRelationMember,
		Role:     OrgRoleMember,
	})
	if err != nil {
		// A concurrent login may have attached it first; the UNIQUE index turns
		// that race into a duplicate-key error, which is a success for us.
		if existing, gErr := GetOrgAccountByUser(user.Id); gErr == nil && existing != nil {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
