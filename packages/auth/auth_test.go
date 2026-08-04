package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestPasswordServiceUsesArgon2id(t *testing.T) {
	service := NewPasswordService()
	hash, err := service.Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if hash == "correct horse battery staple" || len(hash) < 30 {
		t.Fatalf("password was not hashed: %q", hash)
	}
	if err := service.Verify(hash, "correct horse battery staple"); err != nil {
		t.Fatalf("verify password: %v", err)
	}
	if err := service.Verify(hash, "wrong password"); err == nil {
		t.Fatal("wrong password was accepted")
	}
}

func TestTokenServiceValidatesClaimsAndRefreshRotation(t *testing.T) {
	service, err := NewTokenService(TokenConfig{
		Secret:     "test-secret",
		Issuer:     "auction-test",
		Audience:   "auction-api",
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	want := Principal{ID: "user-1", Role: "buyer"}
	if err != nil {
		t.Fatalf("construct token service: %v", err)
	}
	pair, err := service.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue token pair: %v", err)
	}
	got, err := service.Verify(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if got != want {
		t.Fatalf("principal: got %#v, want %#v", got, want)
	}
	if pair.RefreshToken == "" || pair.RefreshTokenHash == "" || pair.RefreshTokenHash == pair.RefreshToken {
		t.Fatal("refresh token was not opaque and hashed")
	}
	rotated, err := service.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue rotated token pair: %v", err)
	}
	if pair.RefreshToken == rotated.RefreshToken {
		t.Fatal("refresh token did not rotate")
	}

	expired, err := NewTokenService(TokenConfig{Secret: "test-secret", Issuer: "auction-test", Audience: "auction-api", AccessTTL: -time.Minute})
	if err != nil {
		t.Fatalf("construct expired token service: %v", err)
	}
	expiredPair, err := expired.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	if _, err := service.Verify(context.Background(), expiredPair.AccessToken); err == nil {
		t.Fatal("expired token was accepted")
	}

	wrongIssuer, err := NewTokenService(TokenConfig{Secret: "test-secret", Issuer: "other", Audience: "auction-api"})
	if err != nil {
		t.Fatalf("construct wrong issuer service: %v", err)
	}
	wrongPair, err := wrongIssuer.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue wrong issuer token: %v", err)
	}
	if _, err := service.Verify(context.Background(), wrongPair.AccessToken); err == nil {
		t.Fatal("wrong issuer token was accepted")
	}

	wrongAudience, err := NewTokenService(TokenConfig{Secret: "test-secret", Issuer: "auction-test", Audience: "other-audience"})
	if err != nil {
		t.Fatalf("construct wrong audience service: %v", err)
	}
	wrongAudiencePair, err := wrongAudience.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue wrong audience token: %v", err)
	}
	if _, err := service.Verify(context.Background(), wrongAudiencePair.AccessToken); err == nil {
		t.Fatal("wrong audience token was accepted")
	}

	withoutExpiration := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":  want.ID,
		"role": want.Role,
		"iss":  "auction-test",
		"aud":  "auction-api",
	})
	missingExpirationToken, err := withoutExpiration.SignedString([]byte("test-secret"))
	if err != nil {
		t.Fatalf("sign token without expiration: %v", err)
	}
	if _, err := service.Verify(context.Background(), missingExpirationToken); err == nil {
		t.Fatal("token without expiration was accepted")
	}
}

func TestTokenServiceRejectsEmptySecret(t *testing.T) {
	if _, err := NewTokenService(TokenConfig{Secret: "  "}); !errors.Is(err, ErrInvalidTokenConfig) {
		t.Fatalf("empty secret error: got %v", err)
	}
}

func TestPasswordServiceRejectsMalformedArgon2Parameters(t *testing.T) {
	service := NewPasswordService()
	salt := base64.RawStdEncoding.EncodeToString([]byte("12345678"))
	hash := base64.RawStdEncoding.EncodeToString(make([]byte, passwordKeyLen))
	for _, encoded := range []string{
		"argon2id$v=19$m=65536,t=3,p=256$" + salt + "$" + hash,
		"argon2id$v=19$m=4294967295,t=3,p=1$" + salt + "$" + hash,
		"argon2id$v=19$m=65536,t=3,p=1,x=1$" + salt + "$" + hash,
		"argon2id$v=19$m=65536,t=3,p=1,p=1$" + salt + "$" + hash,
	} {
		if err := service.Verify(encoded, "password"); err == nil {
			t.Fatalf("malformed Argon2 encoding was accepted: %q", encoded)
		}
	}
}
