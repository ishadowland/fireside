// Package auth implements HS256 JWT issuance and validation plus the
// Sprint 0 stub SMS-code login handler.
//
// The Sprint 0 contract (locked in docs/handoff/sprint0/SUB-001-internal-auth.md):
//
//   POST /v1/auth/login
//     body: {"phone":"+E164","code":"1234"}
//   -> 200 {"token":"<jwt>","expires_in":900}
//   -> 401 {"error":"invalid_credentials"} (wrong code)
//   -> 400 {"error":"invalid_request"} (malformed body)
//
// JWT payload (ADR-0014): {"uid":"<ulid>","jti":"<uuid>", exp:<unix>}.
// UID is a 26-char ULID string (VARCHAR(26) in the users table).
package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

var (
	ErrTokenExpired = errors.New("auth: token expired")
	ErrTokenInvalid = errors.New("auth: token invalid")
)

// Claims is the JWT body. UserID is a 26-char ULID string (ADR-0014).
type Claims struct {
	UserID string `json:"uid"`
	JTI    string `json:"jti"`
	jwt.RegisteredClaims
}

// Issue signs a HS256 token. Returns the encoded token, its jti (so the
// caller can persist it for replay defense — see ADR-0007 §Risks), and any
// signing error. ttl is the access-token lifetime (15 min per RFC).
func Issue(secret []byte, userID string, ttl time.Duration) (string, string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		JTI:    uuid.NewString(),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   "fireside-user",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims)
	signed, err := token.SignedString(secret)
	if err != nil {
		return "", "", err
	}
	return signed, claims.JTI, nil
}

// Validate parses and verifies a HS256 token. Returns ErrTokenExpired for
// expired tokens, ErrTokenInvalid for any other parse/verification failure.
//
// WithExpirationRequired() is passed so a token without an `exp` claim is
// rejected — defense in depth against the (theoretical) case where a
// misconfigured issuer omits expiry.
func Validate(secret []byte, tokenStr string) (*Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithExpirationRequired(),
	)
	claims := &Claims{}
	tok, err := parser.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return secret, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if !tok.Valid {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}