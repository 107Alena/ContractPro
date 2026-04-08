package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"errors"
	"net/http"
	"strings"
	"time"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// clockSkew is the maximum acceptable clock difference between the token
// issuer and this server. Applied as leeway to exp and iat checks.
const clockSkew = 30 * time.Second

// correlationIDHeader is the HTTP header used to propagate request correlation
// IDs across services.
const correlationIDHeader = "X-Correlation-Id"

// Middleware holds the dependencies for JWT authentication.
// Create via NewMiddleware; use Handler() to get the chi-compatible middleware.
type Middleware struct {
	publicKey crypto.PublicKey
	log       *logger.Logger
}

// NewMiddleware constructs a JWT authentication Middleware.
//
// Parameters:
//   - publicKey: the RSA or ECDSA public key used to verify JWT signatures.
//     Must be loaded via LoadPublicKey before calling this function.
//   - log: structured logger for authentication events.
//
// The middleware determines the expected signing algorithm (RS256 or ES256)
// from the public key type at construction time, preventing algorithm
// confusion attacks at request time.
func NewMiddleware(publicKey crypto.PublicKey, log *logger.Logger) (*Middleware, error) {
	// Validate the key type at construction time to fail fast on
	// unsupported keys (e.g., Ed25519) and wrong ECDSA curves.
	if _, err := signingMethodForKey(publicKey); err != nil {
		return nil, err
	}

	return &Middleware{
		publicKey: publicKey,
		log:       log.With("component", "auth-middleware"),
	}, nil
}

// Handler returns a chi-compatible middleware function that:
//  1. Extracts the JWT from the Authorization: Bearer header
//  2. Validates signature, expiration, and custom claims
//  3. Injects AuthContext into context for RBAC middleware
//  4. Injects RequestContext into context for logging/correlation
//  5. Sets the X-Correlation-Id response header
//
// On failure, it writes the appropriate 401 error using model.WriteError
// and does NOT call the next handler.
func (m *Middleware) Handler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Step 1: Generate or preserve correlation ID early so that
			// all error responses (including auth failures) carry it.
			correlationID := r.Header.Get(correlationIDHeader)
			if correlationID == "" {
				correlationID = uuid.New().String()
			}
			w.Header().Set(correlationIDHeader, correlationID)

			// Inject a minimal RequestContext with correlation ID so that
			// WriteError can always emit it in the JSON body.
			ctx := logger.WithRequestContext(r.Context(), logger.RequestContext{
				CorrelationID: correlationID,
			})
			r = r.WithContext(ctx)

			// Step 2: Extract token from Authorization header.
			tokenString, err := extractBearerToken(r)
			if err != nil {
				model.WriteError(w, r, model.ErrAuthTokenMissing, nil)
				return
			}

			// Step 3: Parse and validate the JWT.
			claims, authErr := m.validateToken(tokenString)
			if authErr != nil {
				m.log.Warn(ctx, "JWT validation failed",
					"error", authErr.Error(),
					"method", r.Method,
					"path", r.URL.Path,
				)
				if isExpiredError(authErr) {
					model.WriteError(w, r, model.ErrAuthTokenExpired, nil)
				} else {
					model.WriteError(w, r, model.ErrAuthTokenInvalid, nil)
				}
				return
			}

			// Step 4: Build AuthContext and inject into context.
			ac := AuthContext{
				UserID:         claims.Subject,
				OrganizationID: claims.Org,
				Role:           Role(claims.Role),
				TokenID:        claims.ID,
			}
			ctx = WithAuthContext(ctx, ac)

			// Step 5: Enrich RequestContext with authenticated user info.
			rc := logger.RequestContext{
				CorrelationID:  correlationID,
				OrganizationID: claims.Org,
				UserID:         claims.Subject,
			}
			ctx = logger.WithRequestContext(ctx, rc)

			// Continue with enriched context.
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ValidateToken parses and validates a raw JWT string, returning the claims
// on success. This is exported so the SSE handler (ORCH-TASK-029) can reuse
// the same validation logic for query-param tokens without going through the
// HTTP middleware.
func (m *Middleware) ValidateToken(tokenString string) (*Claims, error) {
	return m.validateToken(tokenString)
}

// validateToken is the internal implementation shared by Handler and
// ValidateToken.
func (m *Middleware) validateToken(tokenString string) (*Claims, error) {
	claims := &Claims{}

	_, err := jwt.ParseWithClaims(tokenString, claims, m.keyFunc,
		jwt.WithLeeway(clockSkew),
		jwt.WithIssuedAt(),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	return claims, nil
}

// keyFunc is the jwt.Keyfunc that returns the pre-loaded public key.
// It also enforces that the token's signing algorithm matches the expected
// method, preventing algorithm confusion attacks (e.g., an attacker sending
// an HS256 token when we expect RS256).
func (m *Middleware) keyFunc(token *jwt.Token) (any, error) {
	switch m.publicKey.(type) {
	case *rsa.PublicKey:
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
	case *ecdsa.PublicKey:
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
	}
	return m.publicKey, nil
}

// extractBearerToken extracts the JWT from the Authorization header.
// Expected format: "Bearer <token>" (scheme comparison is case-insensitive
// per RFC 6750 §1.1 and RFC 9110 §11.1).
// Returns an error if the header is missing or malformed.
func extractBearerToken(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", errors.New("missing Authorization header")
	}

	const prefixLen = len("Bearer ")
	if len(authHeader) < prefixLen || !strings.EqualFold(authHeader[:prefixLen-1], "Bearer") || authHeader[prefixLen-1] != ' ' {
		return "", errors.New("Authorization header must use Bearer scheme")
	}

	token := strings.TrimSpace(authHeader[prefixLen:])
	if token == "" {
		return "", errors.New("empty Bearer token")
	}

	return token, nil
}

// isExpiredError checks whether the JWT validation error is specifically
// about token expiration, so we can return AUTH_TOKEN_EXPIRED instead of
// the generic AUTH_TOKEN_INVALID.
func isExpiredError(err error) bool {
	return errors.Is(err, jwt.ErrTokenExpired)
}
