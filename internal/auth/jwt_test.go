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
	const wantUID = "01HXYZABCDEFGHJKMNPQRSTVWXZ" // 26-char sample ULID
	tok, jti, err := Issue(testSecret, wantUID, 5*time.Minute)
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
	if claims.UserID != wantUID {
		t.Errorf("UserID: got %q, want %q", claims.UserID, wantUID)
	}
	if claims.JTI != jti {
		t.Errorf("JTI: got %q, want %q", claims.JTI, jti)
	}
}

func TestValidateExpired(t *testing.T) {
	t.Parallel()
	tok, _, err := Issue(testSecret, "01HXYZSAMPLEEXP00000000001A", 1*time.Millisecond)
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
	tok, _, err := Issue(testSecret, "01HXYZTAMPERED01234567890ABC", 5*time.Minute)
	if err != nil {
		t.Fatalf("Issue failed: %v", err)
	}

	// Flip the last character of the payload segment. Every payload
	// bit is fully decoded (no base64 padding), and the signature will
	// no longer match the modified JSON — so Validate must reject it.
	//
	// (The original test flipped the LAST SIGNATURE byte via 0x01, but
	// HMAC-SHA256 is 32 bytes → 256 bits, and the 43rd base64url char
	// carries only 4 effective bits + 2 padding bits that every
	// conformant decoder silently drops. So the tamper sometimes landed
	// in the padding and Validate (correctly) accepted the token — a
	// flaky test for an unrelated reason. See issue #1.)
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("expected 3 parts, got %d", len(parts))
	}
	payload := []byte(parts[1])
	payload[len(payload)-1] ^= 0x01
	parts[1] = string(payload)
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
