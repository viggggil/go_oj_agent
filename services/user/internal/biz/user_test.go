package biz

import "testing"

func TestRegisterInputValidate(t *testing.T) {
	policy := DefaultPasswordPolicy()
	input := RegisterInput{
		Username: " alice ",
		Email:    " ALICE@example.com ",
		Password: "correct horse",
	}

	if err := input.Validate(policy); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestRegisterInputNormalize(t *testing.T) {
	input := RegisterInput{
		Username: " alice ",
		Email:    " ALICE@example.com ",
		Password: "correct horse",
	}

	got := input.Normalize()
	if got.Username != "alice" {
		t.Fatalf("Username = %q, want %q", got.Username, "alice")
	}
	if got.Email != "alice@example.com" {
		t.Fatalf("Email = %q, want %q", got.Email, "alice@example.com")
	}
}

func TestPasswordPolicyRejectsShortPassword(t *testing.T) {
	policy := DefaultPasswordPolicy()

	if err := policy.Validate("short"); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestPasswordPolicyRejectsBcryptOverflow(t *testing.T) {
	policy := DefaultPasswordPolicy()
	password := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstu"

	if len(password) <= BcryptMaxPasswordBytes {
		t.Fatalf("test password length = %d, want greater than %d", len(password), BcryptMaxPasswordBytes)
	}
	if err := policy.Validate(password); err == nil {
		t.Fatal("Validate() error = nil, want non-nil")
	}
}

func TestUserHasRole(t *testing.T) {
	user := User{
		ID:    1001,
		Roles: []RoleName{RoleUser, RoleAdmin},
	}

	if !user.HasRole(RoleAdmin) {
		t.Fatal("HasRole(RoleAdmin) = false, want true")
	}
	if user.HasRole(RoleName("unknown")) {
		t.Fatal("HasRole(unknown) = true, want false")
	}
}
