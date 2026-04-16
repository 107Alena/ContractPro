package permissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sony/gobreaker/v2"

	"contractpro/api-orchestrator/internal/config"
	"contractpro/api-orchestrator/internal/egress/opmclient"
	"contractpro/api-orchestrator/internal/infra/observability/logger"
	"contractpro/api-orchestrator/internal/ingress/middleware/auth"
)

// ---------------------------------------------------------------------------
// Consumer-side interfaces — keep Resolver testable without Redis or HTTP.
// ---------------------------------------------------------------------------

// CacheStore provides cache operations for computed permissions.
// Get returns (value, true, nil) on hit, (zero, false, nil) on miss,
// or (zero, false, err) on backend error (treated as miss with WARN).
type CacheStore interface {
	Get(ctx context.Context, orgID string, role auth.Role) (UserPermissions, bool, error)
	Set(ctx context.Context, orgID string, role auth.Role, perms UserPermissions) error
}

// OPMLookup is the subset of opmclient.OPMClient used by the resolver.
type OPMLookup interface {
	ListPolicies(ctx context.Context, orgID string) (json.RawMessage, error)
}

// Metrics is the subset of Prometheus collectors the resolver records to.
// Defined as an interface to allow no-op implementations in tests.
type Metrics interface {
	RecordCacheHit(flag, orgIDHash string)
	RecordCacheMiss(flag string)
	RecordOPMFallback(flag, reason string)
	RecordResolveDuration(seconds float64)
}

// ---------------------------------------------------------------------------
// Resolver
// ---------------------------------------------------------------------------

// Resolver computes UserPermissions from role + OPM policy + env fallback.
// The resolver is non-blocking: on any OPM failure it returns env fallback
// values with a WARN log and increments opm_fallback_total — GET /users/me
// never surfaces an error to the client because of a permissions lookup.
type Resolver struct {
	cache        CacheStore
	opm          OPMLookup
	fallback     config.PermissionsConfig
	log          *logger.Logger
	metrics      Metrics
	breaker      *gobreaker.CircuitBreaker[json.RawMessage]
	opmTimeout   time.Duration
}

// NewResolver constructs a Resolver with the given dependencies. The circuit
// breaker is dedicated to OPM permissions calls (separate state from admin
// proxy calls because the resolver uses an aggressive 2s timeout).
func NewResolver(
	cache CacheStore,
	opm OPMLookup,
	permCfg config.PermissionsConfig,
	cbCfg config.CircuitBreakerConfig,
	metrics Metrics,
	log *logger.Logger,
) *Resolver {
	componentLog := log.With("component", "permissions-resolver")
	cb := gobreaker.NewCircuitBreaker[json.RawMessage](gobreaker.Settings{
		Name:        "permissions-opm",
		MaxRequests: uint32(cbCfg.MaxRequests),
		Timeout:     cbCfg.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(cbCfg.FailureThreshold)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			componentLog.Warn(context.Background(),
				"circuit breaker state change",
				"name", name,
				"from", from.String(),
				"to", to.String(),
			)
		},
	})

	return &Resolver{
		cache:      cache,
		opm:        opm,
		fallback:   permCfg,
		log:        componentLog,
		metrics:    metrics,
		breaker:    cb,
		opmTimeout: permCfg.OPMTimeout,
	}
}

// ResolveForUser returns the computed UserPermissions for a (role, orgID)
// pair. Never returns an error — on OPM failures it logs WARN and returns
// environment fallback values so callers never block on OPM.
//
// Behavior by role:
//   - LAWYER, ORG_ADMIN: ExportEnabled=true (unconditional). No OPM call.
//     No cache read (role-based defaults are trivially computed and would
//     never differ across requests).
//   - BUSINESS_USER: cache → OPM (2s timeout + circuit breaker) → env fallback.
//     Env fallback path does NOT write to cache, so restored OPM health is
//     picked up on the next request.
func (r *Resolver) ResolveForUser(ctx context.Context, role auth.Role, orgID string) UserPermissions {
	start := time.Now()
	defer func() {
		r.metrics.RecordResolveDuration(time.Since(start).Seconds())
	}()

	switch role {
	case auth.RoleLawyer, auth.RoleOrgAdmin:
		return UserPermissions{ExportEnabled: true}
	case auth.RoleBusinessUser:
		return r.resolveBusinessUser(ctx, orgID)
	default:
		// Unknown role — conservative fallback matches env default.
		r.log.Warn(ctx, "unknown role in permissions resolver, using fallback",
			"role", string(role), "organization_id", orgID)
		return UserPermissions{ExportEnabled: r.fallback.OPMFallbackBusinessUserExport}
	}
}

// resolveBusinessUser executes the cache → OPM → env fallback pipeline for
// BUSINESS_USER. See high-architecture.md §6.21.
func (r *Resolver) resolveBusinessUser(ctx context.Context, orgID string) UserPermissions {
	orgHash := hashOrgID(orgID)

	// 1. Cache lookup.
	if perms, ok, err := r.cache.Get(ctx, orgID, auth.RoleBusinessUser); err == nil && ok {
		r.log.Debug(ctx, "permissions cache hit",
			"organization_id", orgID, "role", string(auth.RoleBusinessUser))
		r.metrics.RecordCacheHit(FlagExportEnabled, orgHash)
		return perms
	} else if err != nil {
		r.log.Warn(ctx, "permissions cache lookup error, treating as miss",
			"organization_id", orgID, logger.ErrorAttr(err))
	}
	r.log.Info(ctx, "permissions cache miss, consulting OPM",
		"organization_id", orgID, "role", string(auth.RoleBusinessUser))
	r.metrics.RecordCacheMiss(FlagExportEnabled)

	// 2. OPM lookup with breaker + per-call timeout.
	raw, reason, err := r.fetchFromOPM(ctx, orgID)
	if err != nil {
		r.log.Warn(ctx, "OPM fallback for permissions",
			"organization_id", orgID,
			"reason", reason,
			logger.ErrorAttr(err),
		)
		r.metrics.RecordOPMFallback(FlagExportEnabled, reason)
		return UserPermissions{ExportEnabled: r.fallback.OPMFallbackBusinessUserExport}
	}

	// 3. Parse OPM response.
	enabled, found, parseErr := parseBusinessUserExportFlag(raw)
	if parseErr != nil {
		r.log.Warn(ctx, "OPM fallback for permissions: malformed response",
			"organization_id", orgID, logger.ErrorAttr(parseErr))
		r.metrics.RecordOPMFallback(FlagExportEnabled, FallbackReasonMalformedResponse)
		return UserPermissions{ExportEnabled: r.fallback.OPMFallbackBusinessUserExport}
	}
	if !found {
		r.log.Info(ctx, "business_user_export policy not found in OPM, using env fallback",
			"organization_id", orgID)
		r.metrics.RecordOPMFallback(FlagExportEnabled, FallbackReasonNoPolicy)
		return UserPermissions{ExportEnabled: r.fallback.OPMFallbackBusinessUserExport}
	}

	// 4. Success path — cache the computed value.
	perms := UserPermissions{ExportEnabled: enabled}
	if setErr := r.cache.Set(ctx, orgID, auth.RoleBusinessUser, perms); setErr != nil {
		r.log.Warn(ctx, "failed to cache resolved permissions",
			"organization_id", orgID, logger.ErrorAttr(setErr))
	}
	return perms
}

// fetchFromOPM calls OPM under the circuit breaker with a bounded per-call
// timeout. Returns (raw, "", nil) on success, or (nil, reason, err) where
// reason is a Prometheus label ∈ {timeout, circuit_open, opm_unavailable}.
func (r *Resolver) fetchFromOPM(ctx context.Context, orgID string) (json.RawMessage, string, error) {
	result, err := r.breaker.Execute(func() (json.RawMessage, error) {
		callCtx, cancel := context.WithTimeout(ctx, r.opmTimeout)
		defer cancel()
		return r.opm.ListPolicies(callCtx, orgID)
	})
	if err == nil {
		return result, "", nil
	}
	return nil, classifyOPMError(err), err
}

// classifyOPMError maps an OPM/breaker error to a metric reason label.
// context.Canceled is returned as opm_unavailable (client dropped the request);
// context.DeadlineExceeded is distinguished as timeout.
func classifyOPMError(err error) string {
	if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
		return FallbackReasonCircuitOpen
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return FallbackReasonTimeout
	}
	var opmErr *opmclient.OPMError
	if errors.As(err, &opmErr) {
		if errors.Is(opmErr.Cause, context.DeadlineExceeded) {
			return FallbackReasonTimeout
		}
	}
	return FallbackReasonOPMUnavailable
}

// hashOrgID returns the first 8 hex chars of SHA-256(orgID). Used as the
// `org_id_hash` label on cache_hit_total to keep cardinality bounded.
func hashOrgID(orgID string) string {
	sum := sha256.Sum256([]byte(orgID))
	return hex.EncodeToString(sum[:4])
}

// ---------------------------------------------------------------------------
// Prometheus Metrics adapter
// ---------------------------------------------------------------------------

// PrometheusMetrics adapts the app-level Metrics struct collectors to the
// Resolver's Metrics interface.
type PrometheusMetrics struct {
	CacheHit        *prometheus.CounterVec
	CacheMiss       *prometheus.CounterVec
	OPMFallback     *prometheus.CounterVec
	ResolveDuration prometheus.Histogram
}

// Compile-time interface check.
var _ Metrics = (*PrometheusMetrics)(nil)

// RecordCacheHit increments the cache-hit counter with (flag, org_id_hash).
func (p *PrometheusMetrics) RecordCacheHit(flag, orgIDHash string) {
	p.CacheHit.WithLabelValues(flag, orgIDHash).Inc()
}

// RecordCacheMiss increments the cache-miss counter.
func (p *PrometheusMetrics) RecordCacheMiss(flag string) {
	p.CacheMiss.WithLabelValues(flag).Inc()
}

// RecordOPMFallback increments the fallback counter with (flag, reason).
func (p *PrometheusMetrics) RecordOPMFallback(flag, reason string) {
	p.OPMFallback.WithLabelValues(flag, reason).Inc()
}

// RecordResolveDuration observes a resolve duration in seconds.
func (p *PrometheusMetrics) RecordResolveDuration(seconds float64) {
	p.ResolveDuration.Observe(seconds)
}
