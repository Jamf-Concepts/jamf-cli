// Copyright 2026, Jamf Software LLC

package auth

// ScopeKind is the level a Jamf Platform API integration is created at.
//
// The three levels are mutually exclusive: an integration is minted against one
// of them in Jamf Account, and the credential carries that choice. Crossing over
// is refused — an environment-scoped credential sending X-Tenant-Id, or a
// tenant-scoped one sending X-Environment-Id, gets 403 OWNERSHIP_FORBIDDEN even
// when both IDs belong to the same customer. So this is a choice between
// integrations, not between two ways of naming the same access.
type ScopeKind int

const (
	// ScopeOrganization covers resources belonging to the organization itself
	// (SSO, AI Governance). It sends no header at all: the gateway resolves an
	// organization from the access token, so an org-scoped integration needs a
	// credential and nothing else.
	ScopeOrganization ScopeKind = iota
	// ScopeEnvironment covers a platform environment — a group of tenants across
	// product types. Sent as X-Environment-Id. This is the level to prefer for
	// new integrations.
	ScopeEnvironment
	// ScopeTenant covers a single tenant of Jamf Pro, Jamf School, Jamf Protect
	// or Jamf Security Cloud. Sent as X-Tenant-Id. Jamf Account describes it as
	// the legacy method for targeting integrations without a platform
	// environment; it stays supported, and a tenant-scoped credential must keep
	// using it.
	ScopeTenant
)

// Scope pairs a level with the identifier that names it. The zero value is
// organization scope, which needs no identifier.
type Scope struct {
	Kind ScopeKind
	ID   string
}

// TenantScope returns a tenant-scoped Scope, or organization scope for an empty
// id — so a caller can pass a possibly-unset config value without branching.
func TenantScope(id string) Scope {
	if id == "" {
		return Scope{}
	}
	return Scope{Kind: ScopeTenant, ID: id}
}

// EnvironmentScope returns an environment-scoped Scope, or organization scope
// for an empty id.
func EnvironmentScope(id string) Scope {
	if id == "" {
		return Scope{}
	}
	return Scope{Kind: ScopeEnvironment, ID: id}
}

// Header returns the request header carrying this scope, or ("", "") when there
// is none to send. Organization scope has no header by design, so an empty name
// is a normal answer rather than a missing value.
func (s Scope) Header() (name, value string) {
	if s.ID == "" {
		return "", ""
	}
	switch s.Kind {
	case ScopeEnvironment:
		return "X-Environment-Id", s.ID
	case ScopeTenant:
		return "X-Tenant-Id", s.ID
	default:
		return "", ""
	}
}

// String names the level, for messages that have to say which scope is in play.
func (k ScopeKind) String() string {
	switch k {
	case ScopeEnvironment:
		return "environment"
	case ScopeTenant:
		return "tenant"
	default:
		return "organization"
	}
}
