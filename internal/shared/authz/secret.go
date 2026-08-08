package authz

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// HashSecret SHA-256 hex（小写 64 字符）
func HashSecret(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// MatchSecret 校验明文是否匹配存储值。
// hashed=true 或存值已是 64 位 hex：按 sha256 比较；否则明文 ConstantTimeCompare。
func MatchSecret(plain, stored string, hashed bool) bool {
	plain = strings.TrimSpace(plain)
	stored = strings.TrimSpace(stored)
	if plain == "" || stored == "" {
		return false
	}
	if hashed || looksLikeSHA256Hex(stored) {
		want := HashSecret(plain)
		a, b := []byte(want), []byte(strings.ToLower(stored))
		if len(a) != len(b) {
			return false
		}
		return subtle.ConstantTimeCompare(a, b) == 1
	}
	a, b := []byte(plain), []byte(stored)
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare(a, b) == 1
}

func looksLikeSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range strings.ToLower(s) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
