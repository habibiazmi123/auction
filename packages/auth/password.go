package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	passwordMemory  = 64 * 1024
	passwordTime    = 3
	passwordThreads = 1
	passwordKeyLen  = 32
	passwordSaltLen = 16
)

var ErrInvalidPassword = errors.New("invalid password")

type PasswordService struct{}

func NewPasswordService() *PasswordService { return &PasswordService{} }

func (s *PasswordService) Hash(password string) (string, error) {
	if password == "" {
		return "", ErrInvalidPassword
	}
	salt := make([]byte, passwordSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, passwordTime, passwordMemory, passwordThreads, passwordKeyLen)
	encode := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", passwordMemory, passwordTime, passwordThreads, encode(salt), encode(hash)), nil
}

func (s *PasswordService) Verify(encoded, password string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "argon2id" || parts[1] != "v=19" {
		return ErrInvalidPassword
	}
	params := map[string]uint32{}
	for _, item := range strings.Split(parts[2], ",") {
		keyValue := strings.SplitN(item, "=", 2)
		if len(keyValue) != 2 {
			return ErrInvalidPassword
		}
		value, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil {
			return ErrInvalidPassword
		}
		params[keyValue[0]] = uint32(value)
	}
	if params["m"] == 0 || params["t"] == 0 || params["p"] == 0 {
		return ErrInvalidPassword
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return ErrInvalidPassword
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) == 0 {
		return ErrInvalidPassword
	}
	got := argon2.IDKey([]byte(password), salt, params["t"], params["m"], uint8(params["p"]), uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidPassword
	}
	return nil
}

func (s *PasswordService) Compare(encoded, password string) bool {
	return s.Verify(encoded, password) == nil
}
