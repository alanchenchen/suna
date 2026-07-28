package model

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// SessionScope 从稳定 session ID 派生匿名调用范围，供 Adapter 使用原生缓存或会话优化。
func SessionScope(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("suna:model-invocation:v1\x00" + sessionID))
	return hex.EncodeToString(sum[:16])
}
