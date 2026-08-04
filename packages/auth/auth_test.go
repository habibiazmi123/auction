package auth

import (
	"context"
	"testing"
	"time"
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
	service := NewTokenService(TokenConfig{
		Secret:     "test-secret",
		Issuer:     "auction-test",
		Audience:   "auction-api",
		AccessTTL:  time.Minute,
		RefreshTTL: time.Hour,
	})
	want := Principal{ID: "user-1", Role: "buyer"}
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

	expired := NewTokenService(TokenConfig{Secret: "test-secret", Issuer: "auction-test", Audience: "auction-api", AccessTTL: -time.Minute})
	expiredPair, err := expired.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	if _, err := service.Verify(context.Background(), expiredPair.AccessToken); err == nil {
		t.Fatal("expired token was accepted")
	}

	wrongIssuer := NewTokenService(TokenConfig{Secret: "test-secret", Issuer: "other", Audience: "auction-api"})
	wrongPair, err := wrongIssuer.Issue(context.Background(), want)
	if err != nil {
		t.Fatalf("issue wrong issuer token: %v", err)
	}
	if _, err := service.Verify(context.Background(), wrongPair.AccessToken); err == nil {
		t.Fatal("wrong issuer token was accepted")
	}
}
