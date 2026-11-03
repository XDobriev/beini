package access

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
)

const iterations = 600000

// HashPassword produces a server-only verifier. Callers must not log it.
func HashPassword(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", errors.New("пароль должен содержать от 12 до 1024 байт")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("не удалось создать соль")
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", errors.New("не удалось вычислить хеш")
	}
	return "pbkdf2-sha256$600000$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}

func parseHash(encoded string) ([]byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		return nil, nil, errors.New("некорректный BEINI_TESTER_PASSWORD_HASH")
	}
	salt, e1 := base64.RawStdEncoding.DecodeString(parts[2])
	key, e2 := base64.RawStdEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		return nil, nil, errors.New("некорректный BEINI_TESTER_PASSWORD_HASH")
	}
	return salt, key, nil
}

func CheckPassword(encoded, password string) bool {
	salt, want, err := parseHash(encoded)
	if err != nil || len(password) > 1024 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	return err == nil && subtle.ConstantTimeCompare(want, got) == 1
}
