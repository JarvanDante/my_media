package kit

import (
	"crypto/rand"
	"fmt"
)

// 不含易混字符 0/O/1/I/l
const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz"

// NewPublicID 生成 8 位不重复短码(碰撞概率极低, 落库唯一索引兜底)。
func NewPublicID() (string, error) {
	const n = 8
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	out := make([]byte, n)
	for i := 0; i < n; i++ {
		out[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return string(out), nil
}
