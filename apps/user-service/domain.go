package main

import (
	"context"
	"errors"
	"time"

	"github.com/example/auction/packages/auth"
)

var (
	ErrNotFound            = errors.New("user not found")
	ErrDuplicateEmail      = errors.New("email already registered")
	ErrInvalidEmail        = errors.New("invalid email")
	ErrInvalidRole         = errors.New("invalid role")
	ErrInvalidPassword     = errors.New("invalid password length")
	ErrInvalidCredentials  = errors.New("invalid credentials")
	ErrInvalidRefreshToken = errors.New("invalid refresh token")
	ErrRevokedRefreshToken = errors.New("refresh token revoked")
	ErrExpiredRefreshToken = errors.New("refresh token expired")
)

type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RefreshToken struct {
	Hash      string
	UserID    string
	Role      string
	ExpiresAt time.Time
	RevokedAt *time.Time
}

type UserRepository interface {
	Create(context.Context, User) error
	FindByEmail(context.Context, string) (User, error)
	SaveRefreshTokenHash(context.Context, string, string, time.Time) error
	RevokeRefreshToken(context.Context, string) error
	RotateRefreshToken(context.Context, string, string, string, time.Time) error
}

type refreshFinder interface {
	FindRefreshToken(context.Context, string) (RefreshToken, error)
}

type UserService interface {
	Register(context.Context, string, string, string) (User, error)
	Login(context.Context, string, string) (auth.TokenPair, error)
	Refresh(context.Context, string) (auth.TokenPair, error)
}

type logoutService interface {
	Logout(context.Context, string) error
}
