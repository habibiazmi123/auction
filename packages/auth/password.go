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
	passwordMemory            = 64 * 1024
	passwordTime              = 3
	passwordThreads           = 1
	passwordKeyLen            = 32
	passwordSaltLen           = 16
	passwordMinMemory         = 8
	passwordMaxMemory         = 256 * 1024
	passwordMaxTime           = 10
	passwordMaxThreads uint32 = 32
)

var ErrInvalidPassword = errors.New("invalid password")

type PasswordService struct{}

type argon2Params struct {
	memory      uint32
	time        uint32
	parallelism uint8
}

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
	params, err := parseArgon2Params(parts[2])
	if err != nil {
		return ErrInvalidPassword
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return ErrInvalidPassword
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(want) != passwordKeyLen {
		return ErrInvalidPassword
	}
	got := argon2.IDKey([]byte(password), salt, params.time, params.memory, params.parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidPassword
	}
	return nil
}

func parseArgon2Params(encoded string) (argon2Params, error) {
	var params argon2Params
	seen := make(map[string]bool, 3)
	for _, item := range strings.Split(encoded, ",") {
		keyValue := strings.SplitN(item, "=", 2)
		if len(keyValue) != 2 {
			return argon2Params{}, ErrInvalidPassword
		}
		key := keyValue[0]
		if seen[key] || (key != "m" && key != "t" && key != "p") {
			return argon2Params{}, ErrInvalidPassword
		}
		value, err := strconv.ParseUint(keyValue[1], 10, 32)
		if err != nil {
			return argon2Params{}, ErrInvalidPassword
		}
		seen[key] = true
		switch key {
		case "m":
			params.memory = uint32(value)
		case "t":
			params.time = uint32(value)
		case "p":
			if value == 0 || value > uint64(passwordMaxThreads) {
				return argon2Params{}, ErrInvalidPassword
			}
			params.parallelism = uint8(value)
		}
	}
	if len(seen) != 3 || params.time == 0 || params.time > passwordMaxTime || params.memory < passwordMinMemory || params.memory > passwordMaxMemory || params.memory < 8*uint32(params.parallelism) {
		return argon2Params{}, ErrInvalidPassword
	}
	return params, nil
}

func (s *PasswordService) Compare(encoded, password string) bool {
	return s.Verify(encoded, password) == nil
}
