package main

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/habibiazmi123/auction/packages/auth"
	"github.com/google/uuid"
)

const (
	minimumPasswordLength = 8
	maximumPasswordLength = 128
	dummyPasswordHash     = "argon2id$v=19$m=65536,t=3,p=1$zGKuXczRp89Nln/4FtQ0NQ$4CNUWf50MSeZNltdcUy2W99WlzJmDzhHsuSRAvY2vbE"
)

type passwordService interface {
	Hash(string) (string, error)
	Verify(string, string) error
}

type service struct {
	repository UserRepository
	password   passwordService
	tokens     auth.TokenService
}

func NewUserService(repository UserRepository, password passwordService, tokens auth.TokenService) *service {
	if password == nil {
		password = auth.NewPasswordService()
	}
	return &service{repository: repository, password: password, tokens: tokens}
}

func (s *service) Register(ctx context.Context, email, password, role string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	email = normalizeEmail(email)
	if !validEmail(email) {
		return User{}, ErrInvalidEmail
	}
	if err := validatePassword(password); err != nil {
		return User{}, err
	}
	if !validRole(role) {
		return User{}, ErrInvalidRole
	}
	hash, err := s.password.Hash(password)
	if err != nil {
		return User{}, err
	}
	user := User{ID: uuid.NewString(), Email: email, Role: role, PasswordHash: hash}
	if err := s.repository.Create(ctx, user); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *service) Login(ctx context.Context, email, password string) (auth.TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return auth.TokenPair{}, err
	}
	user, err := s.repository.FindByEmail(ctx, normalizeEmail(email))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = s.password.Verify(dummyPasswordHash, password)
			return auth.TokenPair{}, ErrInvalidCredentials
		}
		return auth.TokenPair{}, err
	}
	if err := s.password.Verify(user.PasswordHash, password); err != nil {
		return auth.TokenPair{}, ErrInvalidCredentials
	}
	pair, err := s.tokens.Issue(ctx, auth.Principal{ID: user.ID, Role: user.Role})
	if err != nil {
		return auth.TokenPair{}, err
	}
	if err := s.repository.SaveRefreshTokenHash(ctx, user.ID, pair.RefreshTokenHash, pair.RefreshExpiresAt); err != nil {
		return auth.TokenPair{}, err
	}
	return pair, nil
}

func (s *service) Refresh(ctx context.Context, refreshToken string) (auth.TokenPair, error) {
	if err := ctx.Err(); err != nil {
		return auth.TokenPair{}, err
	}
	if strings.TrimSpace(refreshToken) == "" {
		return auth.TokenPair{}, ErrInvalidRefreshToken
	}
	finder, ok := s.repository.(refreshFinder)
	if !ok {
		return auth.TokenPair{}, ErrInvalidRefreshToken
	}
	previousHash := auth.HashRefreshToken(refreshToken)
	stored, err := finder.FindRefreshToken(ctx, previousHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return auth.TokenPair{}, ErrInvalidRefreshToken
		}
		return auth.TokenPair{}, err
	}
	if stored.RevokedAt != nil {
		return auth.TokenPair{}, ErrRevokedRefreshToken
	}
	if !stored.ExpiresAt.After(time.Now().UTC()) {
		return auth.TokenPair{}, ErrExpiredRefreshToken
	}
	pair, err := s.tokens.Issue(ctx, auth.Principal{ID: stored.UserID, Role: stored.Role})
	if err != nil {
		return auth.TokenPair{}, err
	}
	err = s.repository.RotateRefreshToken(ctx, previousHash, stored.UserID, pair.RefreshTokenHash, pair.RefreshExpiresAt)
	if err != nil {
		return auth.TokenPair{}, err
	}
	return pair, nil
}

func (s *service) Logout(ctx context.Context, refreshToken string) error {
	if strings.TrimSpace(refreshToken) == "" {
		return ErrInvalidRefreshToken
	}
	if err := s.repository.RevokeRefreshToken(ctx, auth.HashRefreshToken(refreshToken)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrInvalidRefreshToken
		}
		return err
	}
	return nil
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func validEmail(email string) bool {
	parsed, err := mail.ParseAddress(email)
	return err == nil && parsed.Address == email
}

func validRole(role string) bool {
	return role == "buyer" || role == "seller"
}

func validatePassword(password string) error {
	if len(password) < minimumPasswordLength || len(password) > maximumPasswordLength {
		return ErrInvalidPassword
	}
	return nil
}
