package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type deviceClaims struct {
	DongleID string `json:"dongle_id"`
	jwt.RegisteredClaims
}

func IssueDeviceJWT(secret, dongleID string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := deviceClaims{
		DongleID: dongleID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign device jwt: %w", err)
	}
	return signed, nil
}

func ParseDeviceJWT(secret, tokenStr string) (string, error) {
	claims := &deviceClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return "", fmt.Errorf("parse device jwt: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid device jwt")
	}
	if claims.DongleID == "" {
		return "", fmt.Errorf("missing dongle_id claim")
	}
	return claims.DongleID, nil
}
