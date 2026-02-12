package services

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

func normRID(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "none") || v == "" {
		return ""
	}
	return strings.ReplaceAll(v, " ", "")
}

func identityHash(tv, lm string) string {
	tv = normRID(tv)
	lm = normRID(lm)
	if tv == "" || lm == "" {
		return ""
	}
	s := sha256.Sum256([]byte(tv + ":" + lm))
	return hex.EncodeToString(s[:])
}
