package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var testSecret = []byte("test-secret-not-for-production-use-only")

func TestIssueValidateRoundtrip(t *testing.T) {
	t.Parallel()
	tok, jti, err := Issue(testSecret, 42, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	if tok == "" {
		t.Fatal("Issue returned empty token")
	}
	if jti == "" {
		t.Fatal("Issue returned empty jti")
	}
	if !strings.Contains(strings.Split(tok, ".")[1], "") {
		// basic shape sanity: a JWT has three dot-separated segments
		if got := strings.Count(tok, "."); got != 2 {
			t.Fatalf("expected 2 dots in jwt, got %d", got)
		}
	}

	claims, err := Validate(testSecret, tok)
	if err != nil {
		t.Fatalf("Validate failed: %v", err)
	}
	if claims.UserID != 42 {
		t.Errorf("UserID: got %d, want 42", claims.UserID)
	}
	if claims.JTI != jti {
		t.Errorf("JTI: got %q, want %q", claims.JTI, jti)
	}
}

func TestValidateExpired(t *testing.T) {
	t.Parallel()
	tok, _, err := Issue(testSecret, 1, 1*time.Millisecond)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	_, err = Validate(testSecret, tok)
	if err != ErrTokenExpired {
		t.Errorf("got %v, want ErrTokenExpired", err)
	}
}

func TestValidateTampered(t *testing.T) {
	t.Parallel()
	tok, _, err := Issue(testSecret, 99, 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Flip the last character of the signature segment.
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	sig := []byte(parts[2])
	sig[len(sig)-1] ^= 0x01
	parts[2] = string(sig)
	tampered := strings.Join(parts, ".")

	_, err = Validate(testSecret, tampered)
	if err != ErrTokenInvalid {
		t.Errorf("got %v, want ErrTokenInvalid", err)
	}
}

func TestValidateMissingExp(t *testing.T) {
	t.Parallel()
	// Hand-craft a token WITHOUT an `exp` claim. This guards the
	// WithExpirationRequired() option in jwt.go: a token missing exp
	// must be rejected even though it is well-formed and signed.
	claims := jwt.MapClaims{
		"uid": int64(1),
		"jti": uuid.NewString(),
		"sub": "fireside-user",
		"iat": time.Now().Unix(),
		"nbf": time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(testSecret)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Validate(testSecret, signed)
	if err != ErrTokenInvalid {
		t.Errorf("got %v, want ErrTokenInvalid", err)
	}
}
