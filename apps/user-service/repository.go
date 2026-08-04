package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *repository { return &repository{pool: pool} }

func (r *repository) Create(ctx context.Context, user User) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO users (id, email, role, password_hash)
		VALUES ($1, $2, $3, $4)`, user.ID, user.Email, user.Role, user.PasswordHash)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDuplicateEmail
		}
		return fmt.Errorf("create user: %w", err)
	}
	return nil
}

func (r *repository) FindByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := r.pool.QueryRow(ctx, `
		SELECT id, email, role, password_hash, created_at, updated_at
		FROM users WHERE email = $1`, email).Scan(
		&user.ID, &user.Email, &user.Role, &user.PasswordHash, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by email: %w", err)
	}
	return user, nil
}

func (r *repository) SaveRefreshTokenHash(ctx context.Context, userID, tokenHash string, expiresAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`, tokenHash, userID, expiresAt)
	if err != nil {
		return fmt.Errorf("save refresh token: %w", err)
	}
	return nil
}

func (r *repository) FindRefreshToken(ctx context.Context, tokenHash string) (RefreshToken, error) {
	var token RefreshToken
	err := r.pool.QueryRow(ctx, `
		SELECT r.token_hash, r.user_id, u.role, r.expires_at, r.revoked_at
		FROM refresh_tokens r
		JOIN users u ON u.id = r.user_id
		WHERE r.token_hash = $1`, tokenHash).Scan(
		&token.Hash, &token.UserID, &token.Role, &token.ExpiresAt, &token.RevokedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return RefreshToken{}, ErrNotFound
	}
	if err != nil {
		return RefreshToken{}, fmt.Errorf("find refresh token: %w", err)
	}
	return token, nil
}

func (r *repository) RevokeRefreshToken(ctx context.Context, tokenHash string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = COALESCE(revoked_at, now())
		WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke refresh token: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) RotateRefreshToken(ctx context.Context, previousHash, userID, nextHash string, expiresAt time.Time) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL`, previousHash)
	if err != nil {
		return fmt.Errorf("revoke previous refresh token: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrInvalidRefreshToken
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO refresh_tokens (token_hash, user_id, expires_at)
		VALUES ($1, $2, $3)`, nextHash, userID, expiresAt); err != nil {
		return fmt.Errorf("save rotated refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit refresh rotation: %w", err)
	}
	return nil
}
