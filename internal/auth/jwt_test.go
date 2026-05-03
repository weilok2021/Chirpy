package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "test-secret"

func TestMakeJWT_ProducesThreeSegmentToken(t *testing.T) {
	userID := uuid.New()

	tok, err := MakeJWT(userID, testSecret, time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT returned error: %v", err)
	}
	if tok == "" {
		t.Fatal("MakeJWT returned an empty token")
	}
	if parts := strings.Split(tok, "."); len(parts) != 3 {
		t.Fatalf("expected JWT with 3 segments, got %d (%q)", len(parts), tok)
	}
}

func TestValidateJWT_RoundTrip(t *testing.T) {
	userID := uuid.New()

	tok, err := MakeJWT(userID, testSecret, time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}

	got, err := ValidateJWT(tok, testSecret)
	if err != nil {
		t.Fatalf("ValidateJWT returned error: %v", err)
	}
	if got != userID {
		t.Errorf("userID mismatch: want %v, got %v", userID, got)
	}
}

func TestValidateJWT_WrongSecretRejected(t *testing.T) {
	tok, err := MakeJWT(uuid.New(), testSecret, time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}

	if _, err := ValidateJWT(tok, "different-secret"); err == nil {
		t.Error("expected error when validating with wrong secret, got nil")
	}
}

func TestValidateJWT_ExpiredTokenRejected(t *testing.T) {
	tok, err := MakeJWT(uuid.New(), testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}

	if _, err := ValidateJWT(tok, testSecret); err == nil {
		t.Error("expected error for expired token, got nil")
	}
}

func TestValidateJWT_MalformedTokenRejected(t *testing.T) {
	if _, err := ValidateJWT("not.a.real-token", testSecret); err == nil {
		t.Error("expected error for malformed token, got nil")
	}
}

func TestValidateJWT_TamperedPayloadRejected(t *testing.T) {
	tok, err := MakeJWT(uuid.New(), testSecret, time.Minute)
	if err != nil {
		t.Fatalf("MakeJWT failed: %v", err)
	}

	parts := strings.Split(tok, ".")
	tampered := parts[0] + "." + parts[1] + "extra" + "." + parts[2]

	if _, err := ValidateJWT(tampered, testSecret); err == nil {
		t.Error("expected error for tampered token, got nil")
	}
}
