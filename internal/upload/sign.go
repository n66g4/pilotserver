package upload

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Claim struct {
	DongleID string `json:"dongle_id"`
	RelPath  string `json:"rel_path"`
	Exp      int64  `json:"exp"`
}

func Sign(secret string, claim Claim) (string, error) {
	payload, err := json.Marshal(claim)
	if err != nil {
		return "", fmt.Errorf("marshal upload claim: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + signature(secret, encoded), nil
}

func Verify(secret, token string) (Claim, error) {
	var claim Claim
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claim, fmt.Errorf("invalid upload token")
	}
	if !hmac.Equal([]byte(parts[1]), []byte(signature(secret, parts[0]))) {
		return claim, fmt.Errorf("invalid upload token signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return claim, fmt.Errorf("decode upload claim: %w", err)
	}
	if err := json.Unmarshal(payload, &claim); err != nil {
		return claim, fmt.Errorf("unmarshal upload claim: %w", err)
	}
	if claim.DongleID == "" || claim.RelPath == "" || claim.Exp <= time.Now().Unix() {
		return Claim{}, fmt.Errorf("invalid or expired upload claim")
	}
	return claim, nil
}

func signature(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
