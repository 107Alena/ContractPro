// Package permissions implements the Permissions Resolver (UR-10).
//
// The resolver computes user permission flags (e.g. UserPermissions.ExportEnabled)
// from three sources, in order of precedence:
//  1. Role-based default (LAWYER/ORG_ADMIN → always true for export_enabled).
//  2. OPM policy (for BUSINESS_USER: lookup business_user_export.enabled).
//  3. Environment fallback (ORCH_OPM_FALLBACK_BUSINESS_USER_EXPORT).
//
// Computed permissions are cached in Redis per (org_id, role) with TTL
// ORCH_PERMISSIONS_CACHE_TTL. Cache invalidation is triggered via Redis Pub/Sub
// on channel "permissions:invalidate:{org_id}" — published by Admin Proxy after
// PUT /admin/policies/{id}.
//
// Non-blocking by design: if OPM is down or slow, the resolver falls back to
// env defaults and logs WARN. The GET /users/me endpoint never fails because
// of a permissions lookup failure.
package permissions

import (
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// UserPermissions is the computed projection of user permission flags returned
// by GET /users/me inside UserProfile.permissions. See api-specification.yaml
// schema UserPermissions and high-architecture.md §6.21.
type UserPermissions struct {
	// ExportEnabled indicates whether the user may export reports (PDF/DOCX).
	// LAWYER/ORG_ADMIN → always true. BUSINESS_USER → policy-driven.
	ExportEnabled bool `json:"export_enabled"`
}

// FlagExportEnabled is the metric label value for the export_enabled flag.
// Used as the "flag" label value in permissions-related Prometheus counters.
const FlagExportEnabled = "export_enabled"

// KnownRoles lists all roles whose permissions are cached under permissions:{org_id}:{role}.
// Kept in sync with auth.Role values. Cache invalidation iterates this list to
// issue a single multi-key DEL per organization (no KEYS/SCAN required).
var KnownRoles = []auth.Role{
	auth.RoleLawyer,
	auth.RoleBusinessUser,
	auth.RoleOrgAdmin,
}

const (
	cacheKeyPrefix      = "permissions:"
	invalidateChanPref  = "permissions:invalidate:"
	invalidatePattern   = "permissions:invalidate:*"
)

// CacheKey returns the Redis key for cached permissions of (orgID, role).
func CacheKey(orgID string, role auth.Role) string {
	return cacheKeyPrefix + orgID + ":" + string(role)
}

// InvalidateChannel returns the Redis Pub/Sub channel used to invalidate all
// cached permissions entries for a given organization.
func InvalidateChannel(orgID string) string {
	return invalidateChanPref + orgID
}

// InvalidatePattern is the PSUBSCRIBE pattern covering invalidation channels
// for every organization.
func InvalidatePattern() string {
	return invalidatePattern
}

// --- OPM fallback reasons (Prometheus label values) ---

const (
	FallbackReasonTimeout           = "timeout"
	FallbackReasonOPMUnavailable    = "opm_unavailable"
	FallbackReasonCircuitOpen       = "circuit_open"
	FallbackReasonNoPolicy          = "no_policy"
	FallbackReasonMalformedResponse = "malformed_response"
	FallbackReasonCacheCorrupt      = "cache_corrupt"
)
