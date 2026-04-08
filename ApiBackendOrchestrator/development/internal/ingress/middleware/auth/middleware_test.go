package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"contractpro/api-orchestrator/internal/domain/model"
	"contractpro/api-orchestrator/internal/infra/observability/logger"

	"github.com/golang-jwt/jwt/v5"
)

// --- test helpers ---

func testLogger() *logger.Logger {
	return logger.NewLogger("error")
}

// generateRSAKeyPair generates a fresh RSA key pair for testing.
func generateRSAKeyPair(t *testing.T) (*rsa.PrivateKey, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return key, &key.PublicKey
}

// generateECDSAKeyPair generates a fresh ECDSA P-256 key pair for testing.
func generateECDSAKeyPair(t *testing.T) (*ecdsa.PrivateKey, *ecdsa.PublicKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECDSA key: %v", err)
	}
	return key, &key.PublicKey
}

// validClaims returns a Claims struct with all required fields populated
// and an expiration 15 minutes in the future.
func validClaims() Claims {
	now := time.Now()
	return Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "550e8400-e29b-41d4-a716-446655440000",
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
		Org:  "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		Role: "LAWYER",
	}
}

// signToken signs the given claims with the private key using the specified method.
func signToken(t *testing.T, claims Claims, method jwt.SigningMethod, key crypto.PrivateKey) string {
	t.Helper()
	token := jwt.NewWithClaims(method, claims)
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

// newMiddleware creates a Middleware for testing with the given public key.
func newMiddleware(t *testing.T, pub crypto.PublicKey) *Middleware {
	t.Helper()
	m, err := NewMiddleware(pub, testLogger())
	if err != nil {
		t.Fatalf("NewMiddleware: %v", err)
	}
	return m
}

// decodeError decodes the response body into a model.ErrorResponse.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) model.ErrorResponse {
	t.Helper()
	var resp model.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	return resp
}

// echoHandler is a simple handler that writes 200 OK. Used to verify
// that the middleware passes the request through.
var echoHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
})

// --- RS256 tests ---

func TestMiddleware_RS256_ValidToken_PassesThrough(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("got body %q, want ok", rec.Body.String())
	}
}

func TestMiddleware_RS256_InjectsAuthContext(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	var captured AuthContext
	var capturedOk bool
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured, capturedOk = AuthContextFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(handler).ServeHTTP(rec, req)

	if !capturedOk {
		t.Fatal("AuthContext not found in context")
	}
	if captured.UserID != claims.Subject {
		t.Errorf("UserID = %q, want %q", captured.UserID, claims.Subject)
	}
	if captured.OrganizationID != claims.Org {
		t.Errorf("OrganizationID = %q, want %q", captured.OrganizationID, claims.Org)
	}
	if captured.Role != Role(claims.Role) {
		t.Errorf("Role = %q, want %q", captured.Role, claims.Role)
	}
	if captured.TokenID != claims.ID {
		t.Errorf("TokenID = %q, want %q", captured.TokenID, claims.ID)
	}
}

func TestMiddleware_RS256_InjectsRequestContext(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	var capturedRC logger.RequestContext
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRC = logger.RequestContextFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(handler).ServeHTTP(rec, req)

	if capturedRC.UserID != claims.Subject {
		t.Errorf("RC.UserID = %q, want %q", capturedRC.UserID, claims.Subject)
	}
	if capturedRC.OrganizationID != claims.Org {
		t.Errorf("RC.OrganizationID = %q, want %q", capturedRC.OrganizationID, claims.Org)
	}
	if capturedRC.CorrelationID == "" {
		t.Error("RC.CorrelationID should be generated when not provided")
	}
}

// --- ES256 tests ---

func TestMiddleware_ES256_ValidToken_PassesThrough(t *testing.T) {
	priv, pub := generateECDSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodES256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200", rec.Code)
	}
}

func TestMiddleware_ES256_InjectsAuthContext(t *testing.T) {
	priv, pub := generateECDSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodES256, priv)

	var captured AuthContext
	var capturedOk bool
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured, capturedOk = AuthContextFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(handler).ServeHTTP(rec, req)

	if !capturedOk {
		t.Fatal("AuthContext not found in context")
	}
	if captured.Role != RoleLawyer {
		t.Errorf("Role = %q, want LAWYER", captured.Role)
	}
}

// --- Missing token tests ---

func TestMiddleware_MissingAuthorizationHeader_Returns401(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenMissing) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_MISSING", resp.ErrorCode)
	}
}

func TestMiddleware_EmptyBearerToken_Returns401(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenMissing) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_MISSING", resp.ErrorCode)
	}
}

func TestMiddleware_NonBearerScheme_Returns401(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

// --- Expired token tests ---

func TestMiddleware_ExpiredToken_Returns401Expired(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	// Set expiration 5 minutes in the past (beyond 30s clock skew).
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-5 * time.Minute))
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenExpired) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_EXPIRED", resp.ErrorCode)
	}
}

func TestMiddleware_TokenWithinClockSkew_Passes(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	// Set expiration 10 seconds in the past — within 30s clock skew.
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-10 * time.Second))
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 (within clock skew)", rec.Code)
	}
}

// --- Invalid signature tests ---

func TestMiddleware_WrongSigningKey_Returns401Invalid(t *testing.T) {
	priv, _ := generateRSAKeyPair(t)  // sign with this key
	_, pub2 := generateRSAKeyPair(t)  // verify with a different key
	m := newMiddleware(t, pub2)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenInvalid) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_INVALID", resp.ErrorCode)
	}
}

func TestMiddleware_AlgorithmMismatch_Returns401Invalid(t *testing.T) {
	// Sign with ECDSA but verify with RSA key.
	ecPriv, _ := generateECDSAKeyPair(t)
	_, rsaPub := generateRSAKeyPair(t)
	m := newMiddleware(t, rsaPub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodES256, ecPriv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

// --- Invalid claims tests ---

func TestMiddleware_MissingSub_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Subject = ""
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenInvalid) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_INVALID", resp.ErrorCode)
	}
}

func TestMiddleware_InvalidSubUUID_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Subject = "not-a-uuid"
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_MissingOrg_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Org = ""
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_InvalidOrgUUID_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Org = "not-a-uuid"
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_InvalidRole_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Role = "SUPERADMIN"
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_MissingRole_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.Role = ""
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_MissingJTI_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.ID = ""
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

func TestMiddleware_FutureIAT_Returns401Invalid(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	// iat 5 minutes in the future — beyond clock skew.
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(5 * time.Minute))
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

// --- Correlation ID tests ---

func TestMiddleware_GeneratesCorrelationID_WhenAbsent(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	cid := rec.Header().Get("X-Correlation-Id")
	if cid == "" {
		t.Error("X-Correlation-Id header should be set")
	}
	// Should be a valid UUID (36 chars with dashes).
	if len(cid) != 36 {
		t.Errorf("correlation ID %q does not look like a UUID", cid)
	}
}

func TestMiddleware_PreservesCorrelationID_WhenPresent(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	existingCID := "existing-cid-12345678-1234-1234-1234"
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Correlation-Id", existingCID)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Correlation-Id"); got != existingCID {
		t.Errorf("X-Correlation-Id = %q, want %q", got, existingCID)
	}
}

func TestMiddleware_CorrelationIDInRequestContext(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	existingCID := "my-correlation-id"
	var capturedRC logger.RequestContext
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		capturedRC = logger.RequestContextFrom(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Correlation-Id", existingCID)
	rec := httptest.NewRecorder()

	m.Handler()(handler).ServeHTTP(rec, req)

	if capturedRC.CorrelationID != existingCID {
		t.Errorf("CorrelationID = %q, want %q", capturedRC.CorrelationID, existingCID)
	}
}

// --- All roles pass validation ---

func TestMiddleware_AllValidRoles(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	for _, role := range []string{"LAWYER", "BUSINESS_USER", "ORG_ADMIN"} {
		t.Run(role, func(t *testing.T) {
			claims := validClaims()
			claims.Role = role
			token := signToken(t, claims, jwt.SigningMethodRS256, priv)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()

			m.Handler()(echoHandler).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("role %q: got status %d, want 200", role, rec.Code)
			}
		})
	}
}

// --- Malformed token ---

func TestMiddleware_MalformedToken_Returns401Invalid(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer not.a.jwt.token")
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
	resp := decodeError(t, rec)
	if resp.ErrorCode != string(model.ErrAuthTokenInvalid) {
		t.Errorf("error_code = %q, want AUTH_TOKEN_INVALID", resp.ErrorCode)
	}
}

// --- ValidateToken exported method ---

func TestValidateToken_ReturnsClaimsForValidToken(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	got, err := m.ValidateToken(token)
	if err != nil {
		t.Fatalf("ValidateToken: %v", err)
	}
	if got.Subject != claims.Subject {
		t.Errorf("Subject = %q, want %q", got.Subject, claims.Subject)
	}
	if got.Org != claims.Org {
		t.Errorf("Org = %q, want %q", got.Org, claims.Org)
	}
	if got.Role != claims.Role {
		t.Errorf("Role = %q, want %q", got.Role, claims.Role)
	}
}

func TestValidateToken_ReturnsErrorForExpiredToken(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-5 * time.Minute))
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	_, err := m.ValidateToken(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

// --- Error response does not call next handler ---

func TestMiddleware_AuthFailure_DoesNotCallNextHandler(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	called := false
	handler := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	m.Handler()(handler).ServeHTTP(rec, req)

	if called {
		t.Error("next handler should not be called on auth failure")
	}
}

// --- Response content type ---

func TestMiddleware_AuthFailure_SetsJSONContentType(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

// --- "none" algorithm attack ---

func TestMiddleware_NoneAlgorithm_Returns401(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	signed, _ := token.SignedString(jwt.UnsafeAllowNoneSignatureType)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+signed)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("got status %d, want 401", rec.Code)
	}
}

// --- Case-insensitive Bearer scheme ---

func TestMiddleware_LowercaseBearer_PassesThrough(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "bearer "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 (lowercase bearer should work per RFC 6750)", rec.Code)
	}
}

func TestMiddleware_UppercaseBearer_PassesThrough(t *testing.T) {
	priv, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	claims := validClaims()
	token := signToken(t, claims, jwt.SigningMethodRS256, priv)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "BEARER "+token)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("got status %d, want 200 (uppercase BEARER should work per RFC 6750)", rec.Code)
	}
}

// --- Correlation ID on auth error responses ---

func TestMiddleware_AuthError_HasCorrelationID(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	// Missing token → auth error should still have a correlation ID.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got status %d, want 401", rec.Code)
	}

	// Response header should have correlation ID.
	cid := rec.Header().Get("X-Correlation-Id")
	if cid == "" {
		t.Error("X-Correlation-Id header should be set even on auth errors")
	}

	// JSON body should have correlation_id.
	resp := decodeError(t, rec)
	if resp.CorrelationID == "" {
		t.Error("correlation_id in JSON body should be set on auth errors")
	}
	if resp.CorrelationID != cid {
		t.Errorf("correlation_id mismatch: header=%q, body=%q", cid, resp.CorrelationID)
	}
}

func TestMiddleware_AuthError_PreservesIncomingCorrelationID(t *testing.T) {
	_, pub := generateRSAKeyPair(t)
	m := newMiddleware(t, pub)

	existingCID := "incoming-correlation-id-12345678"
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Correlation-Id", existingCID)
	rec := httptest.NewRecorder()

	m.Handler()(echoHandler).ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Correlation-Id"); got != existingCID {
		t.Errorf("X-Correlation-Id = %q, want %q", got, existingCID)
	}

	resp := decodeError(t, rec)
	if resp.CorrelationID != existingCID {
		t.Errorf("correlation_id = %q, want %q", resp.CorrelationID, existingCID)
	}
}
