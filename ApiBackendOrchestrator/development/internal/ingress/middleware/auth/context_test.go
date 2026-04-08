package auth

import (
	"context"
	"testing"
)

func TestAuthContext_RoundTrip(t *testing.T) {
	ac := AuthContext{
		UserID:         "user-123",
		OrganizationID: "org-456",
		Role:           RoleLawyer,
		TokenID:        "jti-789",
	}

	ctx := WithAuthContext(context.Background(), ac)
	got, ok := AuthContextFrom(ctx)

	if !ok {
		t.Fatal("AuthContext not found in context")
	}
	if got.UserID != ac.UserID {
		t.Errorf("UserID = %q, want %q", got.UserID, ac.UserID)
	}
	if got.OrganizationID != ac.OrganizationID {
		t.Errorf("OrganizationID = %q, want %q", got.OrganizationID, ac.OrganizationID)
	}
	if got.Role != ac.Role {
		t.Errorf("Role = %q, want %q", got.Role, ac.Role)
	}
	if got.TokenID != ac.TokenID {
		t.Errorf("TokenID = %q, want %q", got.TokenID, ac.TokenID)
	}
}

func TestAuthContextFrom_EmptyContext(t *testing.T) {
	got, ok := AuthContextFrom(context.Background())

	if ok {
		t.Error("expected ok=false for empty context")
	}
	if got.UserID != "" {
		t.Errorf("expected zero-value, got UserID=%q", got.UserID)
	}
	if got.Role != "" {
		t.Errorf("expected zero-value, got Role=%q", got.Role)
	}
}

func TestIsValidRole(t *testing.T) {
	tests := []struct {
		role Role
		want bool
	}{
		{RoleLawyer, true},
		{RoleBusinessUser, true},
		{RoleOrgAdmin, true},
		{"SUPERADMIN", false},
		{"", false},
		{"lawyer", false}, // case-sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.role), func(t *testing.T) {
			if got := IsValidRole(tt.role); got != tt.want {
				t.Errorf("IsValidRole(%q) = %v, want %v", tt.role, got, tt.want)
			}
		})
	}
}
