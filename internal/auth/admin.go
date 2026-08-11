package auth

import (
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const adminRole = "admin"

type adminClaims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

func CheckAdminPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func IssueAdminJWT(secret string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := adminClaims{
		Role: adminRole,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", fmt.Errorf("sign admin jwt: %w", err)
	}
	return signed, nil
}

func ParseAdminJWT(secret, tokenStr string) error {
	claims := &adminClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return fmt.Errorf("parse admin jwt: %w", err)
	}
	if !token.Valid {
		return fmt.Errorf("invalid admin jwt")
	}
	if claims.Role != adminRole {
		return fmt.Errorf("invalid role claim")
	}
	return nil
}
