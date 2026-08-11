package auth

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type deviceClaims struct {
	Identity string `json:"identity"`
	DongleID string `json:"dongle_id,omitempty"`
	jwt.RegisteredClaims
}

type registerClaims struct {
	Register bool `json:"register"`
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

func VerifyDeviceJWT(tokenStr string, publicKeyForIdentity func(string) (string, error)) (string, error) {
	claims := &deviceClaims{}
	if _, _, err := new(jwt.Parser).ParseUnverified(tokenStr, claims); err != nil {
		return "", fmt.Errorf("parse device jwt claims: %w", err)
	}
	identity := claims.Identity
	if identity == "" {
		identity = claims.DongleID
	}
	if identity == "" {
		return "", fmt.Errorf("missing identity claim")
	}
	publicKeyPEM, err := publicKeyForIdentity(identity)
	if err != nil {
		return "", fmt.Errorf("load device public key: %w", err)
	}
	publicKey, err := parseDevicePublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}

	claims = &deviceClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		switch t.Method {
		case jwt.SigningMethodRS256:
			if _, ok := publicKey.(*rsa.PublicKey); !ok {
				return nil, fmt.Errorf("RSA token requires RSA public key")
			}
		case jwt.SigningMethodES256:
			if _, ok := publicKey.(*ecdsa.PublicKey); !ok {
				return nil, fmt.Errorf("ECDSA token requires ECDSA public key")
			}
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		return "", fmt.Errorf("parse device jwt: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("invalid device jwt")
	}
	verifiedIdentity := claims.Identity
	if verifiedIdentity == "" {
		verifiedIdentity = claims.DongleID
	}
	if verifiedIdentity != identity {
		return "", fmt.Errorf("device identity changed during verification")
	}
	return verifiedIdentity, nil
}

func VerifyRegisterJWT(tokenStr, publicKeyPEM string) error {
	publicKey, err := parseDevicePublicKey(publicKeyPEM)
	if err != nil {
		return err
	}
	claims := &registerClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		switch t.Method {
		case jwt.SigningMethodRS256:
			if _, ok := publicKey.(*rsa.PublicKey); !ok {
				return nil, fmt.Errorf("RSA token requires RSA public key")
			}
		case jwt.SigningMethodES256:
			if _, ok := publicKey.(*ecdsa.PublicKey); !ok {
				return nil, fmt.Errorf("ECDSA token requires ECDSA public key")
			}
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		return fmt.Errorf("parse register jwt: %w", err)
	}
	if !token.Valid || !claims.Register {
		return fmt.Errorf("invalid register jwt")
	}
	return nil
}

func parseDevicePublicKey(publicKeyPEM string) (any, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("invalid device public key PEM")
	}
	if key, err := x509.ParsePKIXPublicKey(block.Bytes); err == nil {
		switch key.(type) {
		case *rsa.PublicKey, *ecdsa.PublicKey:
			return key, nil
		default:
			return nil, fmt.Errorf("unsupported device public key type")
		}
	}
	if key, err := x509.ParsePKCS1PublicKey(block.Bytes); err == nil {
		return key, nil
	}
	return nil, fmt.Errorf("invalid device public key")
}
