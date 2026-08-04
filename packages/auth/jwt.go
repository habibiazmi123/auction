package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/example/auction/packages/contracts"
	"github.com/golang-jwt/jwt/v5"
)

type Principal = contracts.Principal

type TokenService interface {
	Issue(context.Context, Principal) (TokenPair, error)
	Verify(context.Context, string) (Principal, error)
}

type TokenConfig struct {
	Secret     string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type TokenPair struct {
	AccessToken      string
	RefreshToken     string
	RefreshTokenHash string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
}

type JWTService struct {
	secret     []byte
	issuer     string
	audience   string
	accessTTL  time.Duration
	refreshTTL time.Duration
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

var (
	ErrInvalidToken     = errors.New("invalid token")
	ErrInvalidPrincipal = errors.New("invalid principal")
)

func NewTokenService(cfg TokenConfig) *JWTService {
	if cfg.Issuer == "" {
		cfg.Issuer = "auction"
	}
	if cfg.Audience == "" {
		cfg.Audience = "auction-api"
	}
	if cfg.AccessTTL == 0 {
		cfg.AccessTTL = 15 * time.Minute
	}
	if cfg.RefreshTTL == 0 {
		cfg.RefreshTTL = 30 * 24 * time.Hour
	}
	return &JWTService{secret: []byte(cfg.Secret), issuer: cfg.Issuer, audience: cfg.Audience, accessTTL: cfg.AccessTTL, refreshTTL: cfg.RefreshTTL}
}

func NewJWTService(secret, issuer, audience string) *JWTService {
	return NewTokenService(TokenConfig{Secret: secret, Issuer: issuer, Audience: audience})
}

func (s *JWTService) Issue(ctx context.Context, principal Principal) (TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return TokenPair{}, err
	}
	if principal.ID == "" || principal.Role == "" {
		return TokenPair{}, ErrInvalidPrincipal
	}
	now := time.Now().UTC()
	expires := now.Add(s.accessTTL)
	access := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		Role: principal.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   principal.ID,
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	})
	accessToken, err := access.SignedString(s.secret)
	if err != nil {
		return TokenPair{}, fmt.Errorf("sign access token: %w", err)
	}
	refresh, err := newOpaqueToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshExpires := now.Add(s.refreshTTL)
	return TokenPair{
		AccessToken: accessToken, RefreshToken: refresh, RefreshTokenHash: HashRefreshToken(refresh),
		AccessExpiresAt: expires, RefreshExpiresAt: refreshExpires,
	}, nil
}

func (s *JWTService) Verify(ctx context.Context, token string) (Principal, error) {
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}
	parsed, err := jwt.ParseWithClaims(token, &claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithAudience(s.audience), jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return Principal{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	verified, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid || verified.Subject == "" || verified.Role == "" {
		return Principal{}, ErrInvalidToken
	}
	return Principal{ID: verified.Subject, Role: verified.Role}, nil
}

func (s *JWTService) Rotate(ctx context.Context, principal Principal) (TokenPair, error) {
	return s.Issue(ctx, principal)
}

func newOpaqueToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func HashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
