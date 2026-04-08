package auth

import (
	"fmt"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Claims represents the custom JWT claims used by ContractPro.
// It embeds jwt.RegisteredClaims for standard fields (exp, iat, jti)
// and adds application-specific claims (org, role).
type Claims struct {
	jwt.RegisteredClaims

	// Org is the organization ID (UUID) the user belongs to.
	// Mapped from the "org" JWT claim.
	Org string `json:"org"`

	// Role is the user's role within the organization.
	// Must be one of LAWYER, BUSINESS_USER, ORG_ADMIN.
	Role string `json:"role"`
}

// Validate performs application-level validation of the claims beyond what
// the jwt library checks (signature, exp with leeway). This method is called
// automatically by jwt.Parse via the jwt.ClaimsValidator interface.
//
// Checks:
//  1. sub (Subject) is a valid UUID
//  2. org is a valid UUID
//  3. role is one of the recognized values
//  4. jti (ID) is present and non-empty
func (c Claims) Validate() error {
	// 1. sub must be a valid UUID.
	if c.Subject == "" {
		return fmt.Errorf("missing required claim: sub")
	}
	if _, err := uuid.Parse(c.Subject); err != nil {
		return fmt.Errorf("invalid sub claim: not a valid UUID: %w", err)
	}

	// 2. org must be a valid UUID.
	if c.Org == "" {
		return fmt.Errorf("missing required claim: org")
	}
	if _, err := uuid.Parse(c.Org); err != nil {
		return fmt.Errorf("invalid org claim: not a valid UUID: %w", err)
	}

	// 3. role must be one of the recognized values.
	if c.Role == "" {
		return fmt.Errorf("missing required claim: role")
	}
	if !IsValidRole(Role(c.Role)) {
		return fmt.Errorf("invalid role claim: %q", c.Role)
	}

	// 4. jti must be a valid UUID.
	if c.ID == "" {
		return fmt.Errorf("missing required claim: jti")
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return fmt.Errorf("invalid jti claim: not a valid UUID: %w", err)
	}

	return nil
}
