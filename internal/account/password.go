package account

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MinLoginLength            = 3
	MaxLoginLength            = 24
	MinPasswordLength         = 8
	MaxPasswordLength         = 64
	DefaultPasswordIterations = 600_000
	passwordScheme            = "pbkdf2-sha256"
	passwordFieldSeparator    = "$"
	passwordFieldCount        = 4
	passwordSaltBytes         = 16
	passwordKeyBytes          = 32
	temporaryPasswordBytes    = 6
	decoyPassword             = "no account has this login"
)

var loginPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func ValidLogin(login string) bool {
	return len(login) >= MinLoginLength && len(login) <= MaxLoginLength && loginPattern.MatchString(login)
}

func ValidPassword(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= MinPasswordLength && length <= MaxPasswordLength
}

func LoginKey(login string) string {
	return strings.ToLower(login)
}

func hashPassword(password string, iterations int) (string, error) {
	salt, err := randomHex(passwordSaltBytes)
	if err != nil {
		return "", err
	}
	key, err := derivePasswordKey(password, salt, iterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}
	fields := []string{passwordScheme, strconv.Itoa(iterations), salt, hex.EncodeToString(key)}
	return strings.Join(fields, passwordFieldSeparator), nil
}

func derivePasswordKey(password, salt string, iterations, size int) ([]byte, error) {
	key, err := pbkdf2.Key(sha256.New, password, []byte(salt), iterations, size)
	if err != nil {
		return nil, fmt.Errorf("derive password key: %w", err)
	}
	return key, nil
}

func passwordMatches(stored, password string) bool {
	fields := strings.Split(stored, passwordFieldSeparator)
	if len(fields) != passwordFieldCount || fields[0] != passwordScheme {
		return false
	}
	iterations, err := strconv.Atoi(fields[1])
	if err != nil || iterations <= 0 {
		return false
	}
	expected, err := hex.DecodeString(fields[3])
	if err != nil || len(expected) == 0 {
		return false
	}
	candidate, err := derivePasswordKey(password, fields[2], iterations, len(expected))
	return err == nil && subtle.ConstantTimeCompare(candidate, expected) == 1
}
