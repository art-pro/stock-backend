package auth

import "testing"

func TestPasswordHashAndCheck(t *testing.T) {
	t.Parallel()

	hashed, err := HashPassword("super-secret")
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}
	if hashed == "" {
		t.Fatalf("expected non-empty hash")
	}
	if hashed == "super-secret" {
		t.Fatalf("hash must not equal plain password")
	}

	if err := CheckPassword(hashed, "super-secret"); err != nil {
		t.Fatalf("CheckPassword(valid) failed: %v", err)
	}
	if err := CheckPassword(hashed, "wrong-password"); err == nil {
		t.Fatalf("CheckPassword(invalid) expected error")
	}
}

func TestGenerateAndValidateToken(t *testing.T) {
	t.Parallel()

	const secret = "jwt-test-secret"
	token, err := GenerateToken(42, "alice", secret)
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	claims, err := ValidateToken(token, secret)
	if err != nil {
		t.Fatalf("ValidateToken failed: %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("UserID: got %d want %d", claims.UserID, 42)
	}
	if claims.Username != "alice" {
		t.Fatalf("Username: got %q want %q", claims.Username, "alice")
	}
}

func TestValidateTokenWrongSecret(t *testing.T) {
	t.Parallel()

	token, err := GenerateToken(1, "bob", "secret-a")
	if err != nil {
		t.Fatalf("GenerateToken failed: %v", err)
	}

	if _, err := ValidateToken(token, "secret-b"); err == nil {
		t.Fatalf("expected validation error with wrong secret")
	}
}
