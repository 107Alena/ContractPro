package api

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// LimitTypeRead is the label value for read (GET/HEAD) rate limit rejections.
const LimitTypeRead = "read"

// LimitTypeWrite is the label value for write (POST/PUT/DELETE) rate limit rejections.
const LimitTypeWrite = "write"

// OrgRateLimiter maintains per-organization token-bucket rate limiters
// with separate read (GET) and write (POST/PUT/DELETE) budgets.
// Inactive organizations are periodically evicted to bound memory (BRE-009).
type OrgRateLimiter struct {
	mu         sync.Mutex
	orgs       map[string]*orgLimiters
	readRate   rate.Limit
	readBurst  int
	writeRate  rate.Limit
	writeBurst int
	idleTTL    time.Duration
	stopGC     chan struct{}
	doneGC     chan struct{}
	closeOnce  sync.Once
	nowFunc    func() time.Time // injectable clock for testing
}

type orgLimiters struct {
	read     *rate.Limiter
	write    *rate.Limiter
	lastSeen time.Time
}

// NewOrgRateLimiter creates a per-organization rate limiter and starts the
// background cleanup goroutine. Call Close() to stop the cleanup goroutine.
//
// readRPS and writeRPS are the per-organization rates for GET and mutating
// requests respectively. Burst equals the rate (1 second of requests).
// cleanupInterval controls how often stale org entries are evicted.
// idleTTL is how long an org's limiters survive without activity.
func NewOrgRateLimiter(readRPS, writeRPS int, cleanupInterval, idleTTL time.Duration) *OrgRateLimiter {
	if readRPS <= 0 {
		panic("api: readRPS must be positive")
	}
	if writeRPS <= 0 {
		panic("api: writeRPS must be positive")
	}
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Minute
	}
	if idleTTL <= 0 {
		idleTTL = 10 * time.Minute
	}

	o := &OrgRateLimiter{
		orgs:       make(map[string]*orgLimiters),
		readRate:   rate.Limit(readRPS),
		readBurst:  readRPS,
		writeRate:  rate.Limit(writeRPS),
		writeBurst: writeRPS,
		idleTTL:    idleTTL,
		stopGC:     make(chan struct{}),
		doneGC:     make(chan struct{}),
		nowFunc:    time.Now,
	}

	go o.gcLoop(cleanupInterval)
	return o
}

// Close stops the background cleanup goroutine. Safe to call multiple times
// concurrently.
func (o *OrgRateLimiter) Close() {
	o.closeOnce.Do(func() { close(o.stopGC) })
	<-o.doneGC
}

// Allow checks whether the request from orgID should proceed.
// isRead should be true for GET requests, false for POST/PUT/DELETE.
// Returns (allowed, limitType, retryAfterSeconds).
// limitType is "read" or "write".
func (o *OrgRateLimiter) Allow(orgID string, isRead bool) (allowed bool, limitType string, retryAfter int) {
	lims := o.getOrCreate(orgID)

	var limiter *rate.Limiter
	if isRead {
		limitType = LimitTypeRead
		limiter = lims.read
	} else {
		limitType = LimitTypeWrite
		limiter = lims.write
	}

	r := limiter.Reserve()
	if !r.OK() {
		// rate.Inf or zero limit — should never happen with our config.
		return false, limitType, 1
	}

	delay := r.Delay()
	if delay == 0 {
		// Token was available — proceed.
		return true, limitType, 0
	}

	// Token was NOT available. Cancel the reservation (we reject, not wait).
	r.Cancel()
	retryAfter = int(math.Ceil(delay.Seconds()))
	if retryAfter < 1 {
		retryAfter = 1
	}
	return false, limitType, retryAfter
}

// OrgCount returns the number of organizations currently tracked.
// Useful for testing and diagnostics.
func (o *OrgRateLimiter) OrgCount() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.orgs)
}

func (o *OrgRateLimiter) getOrCreate(orgID string) *orgLimiters {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := o.nowFunc()
	lims, ok := o.orgs[orgID]
	if !ok {
		lims = &orgLimiters{
			read:  rate.NewLimiter(o.readRate, o.readBurst),
			write: rate.NewLimiter(o.writeRate, o.writeBurst),
		}
		o.orgs[orgID] = lims
	}
	lims.lastSeen = now
	return lims
}

func (o *OrgRateLimiter) gcLoop(interval time.Duration) {
	defer close(o.doneGC)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			o.evictIdle()
		case <-o.stopGC:
			return
		}
	}
}

func (o *OrgRateLimiter) evictIdle() {
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.nowFunc()
	for orgID, lims := range o.orgs {
		if now.Sub(lims.lastSeen) > o.idleTTL {
			delete(o.orgs, orgID)
		}
	}
}

// RateLimitMetrics is the consumer-side interface for rate limiting metrics.
type RateLimitMetrics interface {
	IncRateLimited(limitType string)
}

// rateLimitMiddleware returns HTTP middleware that enforces per-organization
// rate limits using the provided OrgRateLimiter.
// The middleware reads OrganizationID from the AuthContext (set by authMiddleware).
// orgLimiter may be nil to disable rate limiting (passthrough).
func rateLimitMiddleware(orgLimiter *OrgRateLimiter, metrics RateLimitMetrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if orgLimiter == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ac := authFromContext(r.Context())
			if ac == nil {
				// No auth context — skip rate limiting (auth middleware already
				// returned 401 before this point).
				next.ServeHTTP(w, r)
				return
			}

			isRead := r.Method == http.MethodGet || r.Method == http.MethodHead
			allowed, limitType, retryAfter := orgLimiter.Allow(ac.OrganizationID, isRead)
			if !allowed {
				if metrics != nil {
					metrics.IncRateLimited(limitType)
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeErrorJSON(w, http.StatusTooManyRequests, "RATE_LIMITED",
					"rate limit exceeded, retry after "+strconv.Itoa(retryAfter)+"s")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
