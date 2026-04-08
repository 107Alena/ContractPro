package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestClaims_Validate_AllValid(t *testing.T) {
	c := validClaims()
	if err := c.Validate(); err != nil {
		t.Errorf("expected valid claims, got error: %v", err)
	}
}

func TestClaims_Validate_MissingSub(t *testing.T) {
	c := validClaims()
	c.Subject = ""
	if err := c.Validate(); err == nil {
		t.Error("expected error for missing sub")
	}
}

func TestClaims_Validate_InvalidSubUUID(t *testing.T) {
	c := validClaims()
	c.Subject = "not-a-uuid"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid sub UUID")
	}
}

func TestClaims_Validate_MissingOrg(t *testing.T) {
	c := validClaims()
	c.Org = ""
	if err := c.Validate(); err == nil {
		t.Error("expected error for missing org")
	}
}

func TestClaims_Validate_InvalidOrgUUID(t *testing.T) {
	c := validClaims()
	c.Org = "not-a-uuid"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid org UUID")
	}
}

func TestClaims_Validate_MissingRole(t *testing.T) {
	c := validClaims()
	c.Role = ""
	if err := c.Validate(); err == nil {
		t.Error("expected error for missing role")
	}
}

func TestClaims_Validate_InvalidRole(t *testing.T) {
	c := validClaims()
	c.Role = "SUPERADMIN"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid role")
	}
}

func TestClaims_Validate_MissingJTI(t *testing.T) {
	c := validClaims()
	c.ID = ""
	if err := c.Validate(); err == nil {
		t.Error("expected error for missing jti")
	}
}

func TestClaims_Validate_InvalidJTIUUID(t *testing.T) {
	c := validClaims()
	c.ID = "not-a-uuid"
	if err := c.Validate(); err == nil {
		t.Error("expected error for invalid jti UUID")
	}
}

func TestClaims_Validate_AllValidRoles(t *testing.T) {
	for _, role := range []string{"LAWYER", "BUSINESS_USER", "ORG_ADMIN"} {
		t.Run(role, func(t *testing.T) {
			c := validClaims()
			c.Role = role
			if err := c.Validate(); err != nil {
				t.Errorf("role %q should be valid, got error: %v", role, err)
			}
		})
	}
}

func TestClaims_Validate_ValidSubUUIDs(t *testing.T) {
	validUUIDs := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"00000000-0000-0000-0000-000000000000",
		"ffffffff-ffff-ffff-ffff-ffffffffffff",
	}
	for _, uid := range validUUIDs {
		t.Run(uid, func(t *testing.T) {
			c := validClaims()
			c.Subject = uid
			if err := c.Validate(); err != nil {
				t.Errorf("UUID %q should be valid, got error: %v", uid, err)
			}
		})
	}
}

// validClaims is defined in middleware_test.go, but we need it here too.
// Since both files are in the same test package, it's shared. This test
// file focuses specifically on the Claims.Validate() method.

func TestClaims_Validate_WithRegisteredClaims(t *testing.T) {
	// Verify that Validate does not interfere with registered claims
	// (exp, iat) — those are checked by the jwt library, not by Validate.
	now := time.Now()
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "550e8400-e29b-41d4-a716-446655440000",
			ExpiresAt: jwt.NewNumericDate(now.Add(-1 * time.Hour)), // expired
			IssuedAt:  jwt.NewNumericDate(now),
			ID:        "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
		},
		Org:  "7c9e6679-7425-40de-944b-e07fc1f90ae7",
		Role: "LAWYER",
	}
	// Validate only checks custom claims, not exp.
	if err := c.Validate(); err != nil {
		t.Errorf("Validate should not check exp: %v", err)
	}
}
