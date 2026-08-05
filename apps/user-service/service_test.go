package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/habibiazmi123/auction/packages/auth"
)

type fakeUserRepository struct {
	users       map[string]User
	refresh     map[string]RefreshToken
	rotateCalls int
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{users: make(map[string]User), refresh: make(map[string]RefreshToken)}
}

func (r *fakeUserRepository) Create(_ context.Context, user User) error {
	if _, exists := r.users[user.Email]; exists {
		return ErrDuplicateEmail
	}
	r.users[user.Email] = user
	return nil
}

func (r *fakeUserRepository) FindByEmail(_ context.Context, email string) (User, error) {
	user, ok := r.users[email]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (r *fakeUserRepository) SaveRefreshTokenHash(_ context.Context, userID, tokenHash string, expiresAt time.Time) error {
	r.refresh[tokenHash] = RefreshToken{Hash: tokenHash, UserID: userID, ExpiresAt: expiresAt}
	return nil
}

func (r *fakeUserRepository) FindRefreshToken(_ context.Context, tokenHash string) (RefreshToken, error) {
	token, ok := r.refresh[tokenHash]
	if !ok {
		return RefreshToken{}, ErrNotFound
	}
	return token, nil
}

func (r *fakeUserRepository) RevokeRefreshToken(_ context.Context, tokenHash string) error {
	token, ok := r.refresh[tokenHash]
	if !ok {
		return ErrNotFound
	}
	now := time.Now().UTC()
	token.RevokedAt = &now
	r.refresh[tokenHash] = token
	return nil
}

func (r *fakeUserRepository) RotateRefreshToken(ctx context.Context, previousHash, userID, nextHash string, expiresAt time.Time) error {
	r.rotateCalls++
	if err := r.RevokeRefreshToken(ctx, previousHash); err != nil {
		return err
	}
	return r.SaveRefreshTokenHash(ctx, userID, nextHash, expiresAt)
}

type recordingPasswordService struct {
	verifyCalls    int
	verifiedHash   string
	verifiedSecret string
}

func (s *recordingPasswordService) Hash(string) (string, error) { return "unused", nil }

func (s *recordingPasswordService) Verify(hash, password string) error {
	s.verifyCalls++
	s.verifiedHash = hash
	s.verifiedSecret = password
	return auth.ErrInvalidPassword
}

type fakeTokenService struct{ next int }

func (s *fakeTokenService) Issue(_ context.Context, principal auth.Principal) (auth.TokenPair, error) {
	s.next++
	refresh := "refresh-" + principal.ID + "-" + string(rune('0'+s.next))
	return auth.TokenPair{
		AccessToken:      "access-" + principal.ID,
		RefreshToken:     refresh,
		RefreshTokenHash: auth.HashRefreshToken(refresh),
		AccessExpiresAt:  time.Now().UTC().Add(time.Minute),
		RefreshExpiresAt: time.Now().UTC().Add(time.Hour),
	}, nil
}

func (s *fakeTokenService) Verify(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrInvalidToken
}

func newTestService() (*service, *fakeUserRepository) {
	repo := newFakeUserRepository()
	return NewUserService(repo, auth.NewPasswordService(), &fakeTokenService{}), repo
}

func TestRegisterRejectsDuplicateEmailAndInvalidRole(t *testing.T) {
	service, _ := newTestService()
	if _, err := service.Register(context.Background(), " Alice@example.com ", "correct horse", "buyer"); err != nil {
		t.Fatalf("register user: %v", err)
	}
	if _, err := service.Register(context.Background(), "alice@example.com", "correct horse", "buyer"); !errors.Is(err, ErrDuplicateEmail) {
		t.Fatalf("duplicate email error: got %v", err)
	}
	if _, err := service.Register(context.Background(), "other@example.com", "correct horse", "owner"); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("invalid role error: got %v", err)
	}
}

func TestRegisterRejectsAdminRole(t *testing.T) {
	service, _ := newTestService()
	if _, err := service.Register(context.Background(), "admin@example.com", "correct horse", "admin"); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("admin role error: got %v", err)
	}
}

func TestLoginHidesUnknownEmailAndWrongPassword(t *testing.T) {
	service, _ := newTestService()
	if _, err := service.Register(context.Background(), "buyer@example.com", "correct horse", "buyer"); err != nil {
		t.Fatalf("register user: %v", err)
	}
	_, unknownErr := service.Login(context.Background(), "missing@example.com", "wrong password")
	_, wrongPasswordErr := service.Login(context.Background(), "buyer@example.com", "wrong password")
	if !errors.Is(unknownErr, ErrInvalidCredentials) || !errors.Is(wrongPasswordErr, ErrInvalidCredentials) {
		t.Fatalf("credential errors: unknown=%v wrong-password=%v", unknownErr, wrongPasswordErr)
	}
	if unknownErr.Error() != wrongPasswordErr.Error() {
		t.Fatalf("credential errors disclose account state: %q != %q", unknownErr, wrongPasswordErr)
	}
}

func TestLoginVerifiesUnknownEmailAgainstDummyHash(t *testing.T) {
	passwords := &recordingPasswordService{}
	service := NewUserService(newFakeUserRepository(), passwords, &fakeTokenService{})
	if _, err := service.Login(context.Background(), "missing@example.com", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown email error: got %v", err)
	}
	if passwords.verifyCalls != 1 {
		t.Fatalf("password verification calls: got %d, want 1", passwords.verifyCalls)
	}
	if passwords.verifiedHash != dummyPasswordHash {
		t.Fatalf("verified hash: got %q, want fixed dummy hash", passwords.verifiedHash)
	}
}

func TestLoginReturnsTokensAndPersistsRefreshHash(t *testing.T) {
	service, repo := newTestService()
	user, err := service.Register(context.Background(), "buyer@example.com", "correct horse", "buyer")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	pair, err := service.Login(context.Background(), "buyer@example.com", "correct horse")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("login did not return tokens")
	}
	if _, ok := repo.refresh[pair.RefreshTokenHash]; !ok {
		t.Fatalf("refresh hash was not persisted for user %s", user.ID)
	}
}

func TestRefreshRotatesTokenAndRevokesPreviousToken(t *testing.T) {
	service, repo := newTestService()
	user, err := service.Register(context.Background(), "buyer@example.com", "correct horse", "buyer")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	initial, err := service.Login(context.Background(), user.Email, "correct horse")
	if err != nil {
		t.Fatalf("login user: %v", err)
	}
	rotated, err := service.Refresh(context.Background(), initial.RefreshToken)
	if err != nil {
		t.Fatalf("refresh token: %v", err)
	}
	previous := repo.refresh[initial.RefreshTokenHash]
	if previous.RevokedAt == nil {
		t.Fatal("previous refresh token was not revoked")
	}
	if _, ok := repo.refresh[rotated.RefreshTokenHash]; !ok {
		t.Fatal("rotated refresh token was not persisted")
	}
	if repo.rotateCalls != 1 {
		t.Fatalf("atomic rotation calls: got %d, want 1", repo.rotateCalls)
	}
}

func TestRefreshRejectsRevokedAndExpiredTokens(t *testing.T) {
	service, repo := newTestService()
	user, err := service.Register(context.Background(), "buyer@example.com", "correct horse", "buyer")
	if err != nil {
		t.Fatalf("register user: %v", err)
	}
	now := time.Now().UTC()
	repo.refresh[auth.HashRefreshToken("revoked")] = RefreshToken{Hash: auth.HashRefreshToken("revoked"), UserID: user.ID, Role: user.Role, ExpiresAt: now.Add(time.Hour), RevokedAt: &now}
	repo.refresh[auth.HashRefreshToken("expired")] = RefreshToken{Hash: auth.HashRefreshToken("expired"), UserID: user.ID, Role: user.Role, ExpiresAt: now.Add(-time.Second)}
	if _, err := service.Refresh(context.Background(), "revoked"); !errors.Is(err, ErrRevokedRefreshToken) {
		t.Fatalf("revoked token error: got %v", err)
	}
	if _, err := service.Refresh(context.Background(), "expired"); !errors.Is(err, ErrExpiredRefreshToken) {
		t.Fatalf("expired token error: got %v", err)
	}
}
